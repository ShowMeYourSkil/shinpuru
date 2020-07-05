package database

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/zekroTJA/shinpuru/pkg/multierror"
	"github.com/zekroTJA/shinpuru/pkg/roleutil"

	"github.com/zekroTJA/shinpuru/internal/core/backup/backupmodels"
	"github.com/zekroTJA/shinpuru/internal/core/config"
	"github.com/zekroTJA/shinpuru/internal/core/permissions"
	"github.com/zekroTJA/shinpuru/internal/core/twitchnotify"
	"github.com/zekroTJA/shinpuru/internal/shared/models"
	"github.com/zekroTJA/shinpuru/internal/util"
	"github.com/zekroTJA/shinpuru/internal/util/imgstore"
	"github.com/zekroTJA/shinpuru/internal/util/report"
	"github.com/zekroTJA/shinpuru/internal/util/tag"
	"github.com/zekroTJA/shinpuru/internal/util/vote"

	"github.com/bwmarrin/discordgo"
	"github.com/bwmarrin/snowflake"
	_ "github.com/mattn/go-sqlite3"
)

// SqliteDriver implements the Database interface
// for SQLite.
type SqliteDriver struct {
	db *sql.DB
}

func (m *SqliteDriver) setup() {
	mErr := multierror.New(nil)

	_, err := m.db.Exec("CREATE TABLE IF NOT EXISTS `guilds` (" +
		"`guildID` varchar(25) NOT NULL PRIMARY KEY," +
		"`prefix` text NOT NULL DEFAULT ''," +
		"`autorole` text NOT NULL DEFAULT ''," +
		"`modlogchanID` text NOT NULL DEFAULT ''," +
		"`voicelogchanID` text NOT NULL DEFAULT ''," +
		"`muteRoleID` text NOT NULL DEFAULT ''," +
		"`notifyRoleID` text NOT NULL DEFAULT ''," +
		"`ghostPingMsg` text NOT NULL DEFAULT ''," +
		"`jdoodleToken` text NOT NULL DEFAULT ''," +
		"`backup` text NOT NULL DEFAULT ''," +
		"`inviteBlock` text NOT NULL DEFAULT ''," +
		"`joinMsg` text NOT NULL DEFAULT ''," +
		"`leaveMsg` text NOT NULL DEFAULT ''" +
		");")
	mErr.Append(err)

	_, err = m.db.Exec("CREATE TABLE IF NOT EXISTS `permissions` (" +
		"`roleID` varchar(25) NOT NULL PRIMARY KEY," +
		"`guildID` text NOT NULL DEFAULT ''," +
		"`permission` text NOT NULL DEFAULT ''" +
		");")
	mErr.Append(err)

	_, err = m.db.Exec("CREATE TABLE IF NOT EXISTS `reports` (" +
		"`id` varchar(25) NOT NULL PRIMARY KEY," +
		"`type` int(11) NOT NULL DEFAULT '3'," +
		"`guildID` text NOT NULL DEFAULT ''," +
		"`executorID` text NOT NULL DEFAULT ''," +
		"`victimID` text NOT NULL DEFAULT ''," +
		"`msg` text NOT NULL DEFAULT ''," +
		"`attachment` text NOT NULL DEFAULT ''" +
		");")
	mErr.Append(err)

	_, err = m.db.Exec("CREATE TABLE IF NOT EXISTS `settings` (" +
		"`iid` INTEGER PRIMARY KEY AUTOINCREMENT," +
		"`setting` text NOT NULL DEFAULT ''," +
		"`value` text NOT NULL DEFAULT ''" +
		");")
	mErr.Append(err)

	_, err = m.db.Exec("CREATE TABLE IF NOT EXISTS `votes` (" +
		"`id` varchar(25) NOT NULL PRIMARY KEY," +
		"`data` mediumtext NOT NULL DEFAULT ''" +
		");")
	mErr.Append(err)

	_, err = m.db.Exec("CREATE TABLE IF NOT EXISTS `twitchnotify` (" +
		"`iid` INTEGER PRIMARY KEY AUTOINCREMENT," +
		"`guildID` text NOT NULL DEFAULT ''," +
		"`channelID` text NOT NULL DEFAULT ''," +
		"`twitchUserID` text NOT NULL DEFAULT ''" +
		");")
	mErr.Append(err)

	_, err = m.db.Exec("CREATE TABLE IF NOT EXISTS `backups` (" +
		"`iid` INTEGER PRIMARY KEY AUTOINCREMENT," +
		"`guildID` text NOT NULL DEFAULT ''," +
		"`timestamp` bigint(20) NOT NULL DEFAULT 0," +
		"`fileID` text NOT NULL DEFAULT ''" +
		");")
	mErr.Append(err)

	_, err = m.db.Exec("CREATE TABLE IF NOT EXISTS `tags` (" +
		"`id` varchar(25) NOT NULL PRIMARY KEY," +
		"`ident` text NOT NULL DEFAULT ''," +
		"`creatorID` text NOT NULL DEFAULT ''," +
		"`guildID` text NOT NULL DEFAULT ''," +
		"`content` text NOT NULL DEFAULT ''," +
		"`created` bigint(20) NOT NULL DEFAULT 0," +
		"`lastEdit` bigint(20) NOT NULL DEFAULT 0" +
		");")
	mErr.Append(err)

	_, err = m.db.Exec("CREATE TABLE IF NOT EXISTS `sessions` (" +
		"`iid` INTEGER PRIMARY KEY AUTOINCREMENT," +
		"`sessionkey` text NOT NULL DEFAULT ''," +
		"`userID` text NOT NULL DEFAULT ''," +
		"`expires` datetime NOT NULL DEFAULT CURRENT_TIMESTAMP" +
		");")
	mErr.Append(err)

	_, err = m.db.Exec("CREATE TABLE IF NOT EXISTS `apitokens` (" +
		"`userID` varchar(25) NOT NULL PRIMARY KEY," +
		"`salt` text NOT NULL," +
		"`created` timestamp NOT NULL," +
		"`expires` timestamp NOT NULL," +
		"`lastAccess` timestamp NOT NULL," +
		"`hits` bigint(20) NOT NULL" +
		");")
	mErr.Append(err)

	if mErr.Len() > 0 {
		util.Log.Fatalf("Failed database setup: %s", mErr.Concat().Error())
	}
}

func (m *SqliteDriver) Connect(credentials ...interface{}) error {
	var err error
	creds := credentials[0].(*config.DatabaseFile)
	if creds == nil {
		return errors.New("Database credentials from config were nil")
	}
	dsn := fmt.Sprintf("file:" + creds.DBFile)
	m.db, err = sql.Open("sqlite3", dsn)
	m.setup()
	return err
}

func (m *SqliteDriver) Close() {
	if m.db != nil {
		m.db.Close()
	}
}

func (m *SqliteDriver) getGuildSetting(guildID, key string) (string, error) {
	var value string
	err := m.db.QueryRow(
		fmt.Sprintf("SELECT %s FROM guilds WHERE guildID = ?", key),
		guildID).Scan(&value)
	if err == sql.ErrNoRows {
		err = ErrDatabaseNotFound
	}
	return value, err
}

func (m *SqliteDriver) setGuildSetting(guildID, key string, value string) (err error) {
	res, err := m.db.Exec(
		fmt.Sprintf("UPDATE guilds SET %s = ? WHERE guildID = ?", key),
		value, guildID)
	if err != nil {
		return
	}

	ar, err := res.RowsAffected()
	if err != nil {
		return
	}
	if ar == 0 {
		_, err = m.db.Exec(
			fmt.Sprintf("INSERT INTO guilds (guildID, %s) VALUES (?, ?)", key),
			guildID, value)
	}

	return nil
}

func (m *SqliteDriver) GetGuildPrefix(guildID string) (string, error) {
	val, err := m.getGuildSetting(guildID, "prefix")
	return val, err
}

func (m *SqliteDriver) SetGuildPrefix(guildID, newPrefix string) error {
	return m.setGuildSetting(guildID, "prefix", newPrefix)
}

func (m *SqliteDriver) GetGuildAutoRole(guildID string) (string, error) {
	val, err := m.getGuildSetting(guildID, "autorole")
	return val, err
}

func (m *SqliteDriver) SetGuildAutoRole(guildID, autoRoleID string) error {
	return m.setGuildSetting(guildID, "autorole", autoRoleID)
}

func (m *SqliteDriver) GetGuildModLog(guildID string) (string, error) {
	val, err := m.getGuildSetting(guildID, "modlogchanID")
	return val, err
}

func (m *SqliteDriver) SetGuildModLog(guildID, chanID string) error {
	return m.setGuildSetting(guildID, "modlogchanID", chanID)
}

func (m *SqliteDriver) GetGuildVoiceLog(guildID string) (string, error) {
	val, err := m.getGuildSetting(guildID, "voicelogchanID")
	return val, err
}

func (m *SqliteDriver) SetGuildVoiceLog(guildID, chanID string) error {
	return m.setGuildSetting(guildID, "voicelogchanID", chanID)
}

func (m *SqliteDriver) GetGuildNotifyRole(guildID string) (string, error) {
	val, err := m.getGuildSetting(guildID, "notifyRoleID")
	return val, err
}

func (m *SqliteDriver) SetGuildNotifyRole(guildID, roleID string) error {
	return m.setGuildSetting(guildID, "notifyRoleID", roleID)
}

func (m *SqliteDriver) GetGuildGhostpingMsg(guildID string) (string, error) {
	val, err := m.getGuildSetting(guildID, "ghostPingMsg")
	return val, err
}

func (m *SqliteDriver) SetGuildGhostpingMsg(guildID, msg string) error {
	return m.setGuildSetting(guildID, "ghostPingMsg", msg)
}

func (m *SqliteDriver) GetMemberPermission(s *discordgo.Session, guildID string, memberID string) (permissions.PermissionArray, error) {
	guildPerms, err := m.GetGuildPermissions(guildID)
	if err != nil {
		return nil, err
	}

	membRoles, err := roleutil.GetSortedMemberRoles(s, guildID, memberID, false)
	if err != nil {
		return nil, err
	}

	var res permissions.PermissionArray
	for _, r := range membRoles {
		if p, ok := guildPerms[r.ID]; ok {
			if res == nil {
				res = p
			} else {
				res = res.Merge(p, true)
			}
		}
	}

	return res, nil
}

func (m *SqliteDriver) GetGuildPermissions(guildID string) (map[string]permissions.PermissionArray, error) {
	results := make(map[string]permissions.PermissionArray)
	rows, err := m.db.Query("SELECT roleID, permission FROM permissions WHERE guildID = ?",
		guildID)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var roleID string
		var permission string
		err := rows.Scan(&roleID, &permission)
		if err != nil {
			return nil, err
		}
		results[roleID] = strings.Split(permission, ",")
	}
	return results, nil
}

func (m *SqliteDriver) SetGuildRolePermission(guildID, roleID string, p permissions.PermissionArray) error {
	if len(p) == 0 {
		_, err := m.db.Exec("DELETE FROM permissions WHERE roleID = ?", roleID)
		return err
	}

	pStr := strings.Join(p, ",")
	res, err := m.db.Exec("UPDATE permissions SET permission = ? WHERE roleID = ? AND guildID = ?",
		pStr, roleID, guildID)
	if err != nil {
		return err
	}
	ar, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if ar == 0 {
		_, err = m.db.Exec("INSERT INTO permissions (roleID, guildID, permission) VALUES (?, ?, ?)",
			roleID, guildID, pStr)
	}
	return err
}

func (m *SqliteDriver) GetGuildJdoodleKey(guildID string) (string, error) {
	val, err := m.getGuildSetting(guildID, "jdoodleToken")
	return val, err
}

func (m *SqliteDriver) SetGuildJdoodleKey(guildID, key string) error {
	return m.setGuildSetting(guildID, "jdoodleToken", key)
}

func (m *SqliteDriver) GetGuildBackup(guildID string) (bool, error) {
	val, err := m.getGuildSetting(guildID, "backup")
	return val != "", err
}

func (m *SqliteDriver) SetGuildBackup(guildID string, enabled bool) error {
	var val string
	if enabled {
		val = "1"
	}
	return m.setGuildSetting(guildID, "backup", val)
}

func (m *SqliteDriver) GetSetting(setting string) (string, error) {
	var value string
	err := m.db.QueryRow("SELECT value FROM settings WHERE setting = ?", setting).Scan(&value)
	if err == sql.ErrNoRows {
		err = ErrDatabaseNotFound
	}
	return value, err
}

func (m *SqliteDriver) SetSetting(setting, value string) error {
	res, err := m.db.Exec("UPDATE settings SET value = ? WHERE setting = ?", value, setting)
	ar, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if ar == 0 {
		_, err = m.db.Exec("INSERT INTO settings (setting, value) VALUES (?, ?)", setting, value)
	}
	return err
}

func (m *SqliteDriver) AddReport(rep *report.Report) error {
	_, err := m.db.Exec("INSERT INTO reports (id, type, guildID, executorID, victimID, msg, attachment) VALUES (?, ?, ?, ?, ?, ?, ?)",
		rep.ID, rep.Type, rep.GuildID, rep.ExecutorID, rep.VictimID, rep.Msg, rep.AttachmehtURL)
	return err
}

func (m *SqliteDriver) DeleteReport(id snowflake.ID) error {
	_, err := m.db.Exec("DELETE FROM reports WHERE id = ?", id)
	return err
}

func (m *SqliteDriver) GetReport(id snowflake.ID) (*report.Report, error) {
	rep := new(report.Report)

	row := m.db.QueryRow("SELECT id, type, guildID, executorID, victimID, msg, attachment FROM reports WHERE id = ?", id)
	err := row.Scan(&rep.ID, &rep.Type, &rep.GuildID, &rep.ExecutorID, &rep.VictimID, &rep.Msg, &rep.AttachmehtURL)
	if err == sql.ErrNoRows {
		return nil, ErrDatabaseNotFound
	}

	return rep, err
}

func (m *SqliteDriver) GetReportsGuild(guildID string, offset, limit int) ([]*report.Report, error) {
	if limit == 0 {
		limit = 1000
	}

	rows, err := m.db.Query(
		"SELECT id, type, guildID, executorID, victimID, msg, attachment "+
			"FROM reports WHERE guildID = ? "+
			"ORDER BY iid DESC "+
			"LIMIT ?, ?", guildID, offset, limit)
	var results []*report.Report
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		rep := new(report.Report)
		err := rows.Scan(&rep.ID, &rep.Type, &rep.GuildID, &rep.ExecutorID, &rep.VictimID, &rep.Msg, &rep.AttachmehtURL)
		if err != nil {
			return nil, err
		}
		results = append(results, rep)
	}
	return results, nil
}

func (m *SqliteDriver) GetReportsFiltered(guildID, memberID string, repType int) ([]*report.Report, error) {
	if !util.IsNumber(guildID) || !util.IsNumber(memberID) {
		return nil, fmt.Errorf("invalid argument type")
	}

	query := fmt.Sprintf(`SELECT id, type, guildID, executorID, victimID, msg, attachment FROM reports WHERE guildID = "%s"`, guildID)
	if memberID != "" {
		query += fmt.Sprintf(` AND victimID = "%s"`, memberID)
	}
	if repType != -1 {
		query += fmt.Sprintf(` AND type = %d`, repType)
	}
	rows, err := m.db.Query(query)
	var results []*report.Report
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		rep := new(report.Report)
		err := rows.Scan(&rep.ID, &rep.Type, &rep.GuildID, &rep.ExecutorID, &rep.VictimID, &rep.Msg, &rep.AttachmehtURL)
		if err != nil {
			return nil, err
		}
		results = append(results, rep)
	}
	return results, nil
}

func (m *SqliteDriver) GetReportsGuildCount(guildID string) (count int, err error) {
	err = m.db.QueryRow("SELECT COUNT(id) FROM reports WHERE guildID = ?", guildID).Scan(&count)
	return
}

func (m *SqliteDriver) GetReportsFilteredCount(guildID, memberID string, repType int) (count int, err error) {
	if !util.IsNumber(guildID) || !util.IsNumber(memberID) {
		err = fmt.Errorf("invalid argument type")
		return
	}

	query := fmt.Sprintf(`SELECT COUNT(id) FROM reports WHERE guildID = "%s"`, guildID)
	if memberID != "" {
		query += fmt.Sprintf(` AND victimID = "%s"`, memberID)
	}
	if repType != -1 {
		query += fmt.Sprintf(` AND type = %d`, repType)
	}

	err = m.db.QueryRow(query).Scan(&count)
	return
}

func (m *SqliteDriver) GetVotes() (map[string]*vote.Vote, error) {
	rows, err := m.db.Query("SELECT id, data FROM votes")
	results := make(map[string]*vote.Vote)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var voteID, rawData string
		err := rows.Scan(&voteID, &rawData)
		if err != nil {
			util.Log.Error("An error occured reading vote from database: ", err)
			continue
		}
		vote, err := vote.Unmarshal(rawData)
		if err != nil {
			m.DeleteVote(rawData)
		} else {
			results[vote.ID] = vote
		}
	}
	return results, err
}

func (m *SqliteDriver) AddUpdateVote(vote *vote.Vote) error {
	rawData, err := vote.Marshal()
	if err != nil {
		return err
	}

	res, err := m.db.Exec("UPDATE votes SET data = ? WHERE id = ?", rawData, vote.ID)
	ar, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if ar == 0 {
		_, err = m.db.Exec("INSERT INTO votes (id, data) VALUES (?, ?)", vote.ID, rawData)
	}

	return err
}

func (m *SqliteDriver) DeleteVote(voteID string) error {
	_, err := m.db.Exec("DELETE FROM votes WHERE id = ?", voteID)
	return err
}

func (m *SqliteDriver) GetMuteRoles() (map[string]string, error) {
	rows, err := m.db.Query("SELECT guildID, muteRoleID FROM guilds")
	results := make(map[string]string)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var guildID, roleID string
		err = rows.Scan(&guildID, &roleID)
		if err == nil {
			results[guildID] = roleID
		}
	}
	return results, nil
}

func (m *SqliteDriver) GetMuteRoleGuild(guildID string) (string, error) {
	val, err := m.getGuildSetting(guildID, "muteRoleID")
	return val, err
}

func (m *SqliteDriver) SetMuteRole(guildID, roleID string) error {
	return m.setGuildSetting(guildID, "muteRoleID", roleID)
}

func (m *SqliteDriver) GetTwitchNotify(twitchUserID, guildID string) (*twitchnotify.DBEntry, error) {
	t := &twitchnotify.DBEntry{
		TwitchUserID: twitchUserID,
		GuildID:      guildID,
	}
	err := m.db.QueryRow("SELECT channelID FROM twitchnotify WHERE twitchUserID = ? AND guildID = ?",
		twitchUserID, guildID).Scan(&t.ChannelID)
	if err == sql.ErrNoRows {
		err = ErrDatabaseNotFound
	}
	return t, err
}

func (m *SqliteDriver) SetTwitchNotify(twitchNotify *twitchnotify.DBEntry) error {
	res, err := m.db.Exec("UPDATE twitchnotify SET channelID = ? WHERE twitchUserID = ? AND guildID = ?",
		twitchNotify.ChannelID, twitchNotify.TwitchUserID, twitchNotify.GuildID)
	if err != nil {
		return err
	}
	ar, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if ar == 0 {
		_, err = m.db.Exec("INSERT INTO twitchnotify (twitchUserID, guildID, channelID) VALUES (?, ?, ?)",
			twitchNotify.TwitchUserID, twitchNotify.GuildID, twitchNotify.ChannelID)
	}
	return err
}

func (m *SqliteDriver) DeleteTwitchNotify(twitchUserID, guildID string) error {
	_, err := m.db.Exec("DELETE FROM twitchnotify WHERE twitchUserID = ? AND guildID = ?", twitchUserID, guildID)
	return err
}

func (m *SqliteDriver) GetAllTwitchNotifies(twitchUserID string) ([]*twitchnotify.DBEntry, error) {
	query := "SELECT twitchUserID, guildID, channelID FROM twitchnotify"
	if twitchUserID != "" {
		query += " WHERE twitchUserID = " + twitchUserID
	}
	rows, err := m.db.Query(query)
	results := make([]*twitchnotify.DBEntry, 0)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		t := new(twitchnotify.DBEntry)
		err = rows.Scan(&t.TwitchUserID, &t.GuildID, &t.ChannelID)
		if err == nil {
			results = append(results, t)
		}
	}
	return results, nil
}

func (m *SqliteDriver) AddBackup(guildID, fileID string) error {
	timestamp := time.Now().Unix()
	_, err := m.db.Exec("INSERT INTO backups (guildID, timestamp, fileID) VALUES (?, ?, ?)", guildID, timestamp, fileID)
	return err
}

func (m *SqliteDriver) DeleteBackup(guildID, fileID string) error {
	_, err := m.db.Exec("DELETE FROM backups WHERE guildID = ? AND fileID = ?", guildID, fileID)
	return err
}

func (m *SqliteDriver) GetGuildInviteBlock(guildID string) (string, error) {
	return m.getGuildSetting(guildID, "inviteBlock")
}

func (m *SqliteDriver) SetGuildInviteBlock(guildID string, data string) error {
	return m.setGuildSetting(guildID, "inviteBlock", data)
}

func (m *SqliteDriver) GetGuildJoinMsg(guildID string) (string, string, error) {
	data, err := m.getGuildSetting(guildID, "joinMsg")
	if err != nil {
		return "", "", err
	}
	if data == "" {
		return "", "", nil
	}

	i := strings.Index(data, "|")
	if i < 0 || len(data) < i+1 {
		return "", "", nil
	}

	return data[:i], data[i+1:], nil
}

func (m *SqliteDriver) SetGuildJoinMsg(guildID string, msg string, channelID string) error {
	return m.setGuildSetting(guildID, "joinMsg", fmt.Sprintf("%s|%s", msg, channelID))
}

func (m *SqliteDriver) GetGuildLeaveMsg(guildID string) (string, string, error) {
	data, err := m.getGuildSetting(guildID, "leaveMsg")
	if err != nil {
		return "", "", err
	}
	if data == "" {
		return "", "", nil
	}

	i := strings.Index(data, "|")
	if i < 0 || len(data) < i+1 {
		return "", "", nil
	}

	return data[:i], data[i+1:], nil
}

func (m *SqliteDriver) SetGuildLeaveMsg(guildID string, channelID string, msg string) error {
	return m.setGuildSetting(guildID, "leaveMsg", fmt.Sprintf("%s|%s", channelID, msg))
}

func (m *SqliteDriver) GetBackups(guildID string) ([]*backupmodels.Entry, error) {
	rows, err := m.db.Query("SELECT guildID, timestamp, fileID FROM backups WHERE guildID = ?", guildID)
	if err == sql.ErrNoRows {
		return nil, ErrDatabaseNotFound
	}
	if err != nil {
		return nil, err
	}

	backups := make([]*backupmodels.Entry, 0)
	for rows.Next() {
		be := new(backupmodels.Entry)
		var timeStampUnix int64
		err = rows.Scan(&be.GuildID, &timeStampUnix, &be.FileID)
		if err != nil {
			return nil, err
		}
		be.Timestamp = time.Unix(timeStampUnix, 0)
		backups = append(backups, be)
	}

	return backups, nil
}

func (m *SqliteDriver) GetGuilds() ([]string, error) {
	rows, err := m.db.Query("SELECT guildID FROM guilds WHERE backup = '1'")
	if err == sql.ErrNoRows {
		return nil, ErrDatabaseNotFound
	}
	if err != nil {
		return nil, err
	}

	guilds := make([]string, 0)
	for rows.Next() {
		var s string
		err = rows.Scan(&s)
		if err != nil {
			return nil, err
		}
		guilds = append(guilds, s)
	}

	return guilds, err
}

func (m *SqliteDriver) AddTag(tag *tag.Tag) error {
	_, err := m.db.Exec("INSERT INTO tags (id, ident, creatorID, guildID, content, created, lastEdit) VALUES "+
		"(?, ?, ?, ?, ?, ?, ?)", tag.ID, tag.Ident, tag.CreatorID, tag.GuildID, tag.Content, tag.Created.Unix(), tag.LastEdit.Unix())
	return err
}

func (m *SqliteDriver) EditTag(tag *tag.Tag) error {
	_, err := m.db.Exec("UPDATE tags SET "+
		"ident = ?, creatorID = ?, guildID = ?, content = ?, created = ?, lastEdit = ? "+
		"WHERE id = ?", tag.Ident, tag.CreatorID, tag.GuildID, tag.Content, tag.Created.Unix(), tag.LastEdit.Unix(), tag.ID)
	if err == sql.ErrNoRows {
		return ErrDatabaseNotFound
	}
	return err
}

func (m *SqliteDriver) GetTagByID(id snowflake.ID) (*tag.Tag, error) {
	tag := new(tag.Tag)
	var timestampCreated int64
	var timestampLastEdit int64

	row := m.db.QueryRow("SELECT id, ident, creatorID, guildID, content, created, lastEdit FROM tags "+
		"WHERE id = ?", id)

	err := row.Scan(&tag.ID, &tag.Ident, &tag.CreatorID, &tag.GuildID,
		&tag.Content, &timestampCreated, &timestampLastEdit)
	if err == sql.ErrNoRows {
		return nil, ErrDatabaseNotFound
	}
	if err != nil {
		return nil, err
	}

	tag.Created = time.Unix(timestampCreated, 0)
	tag.LastEdit = time.Unix(timestampLastEdit, 0)

	return tag, nil
}

func (m *SqliteDriver) GetTagByIdent(ident string, guildID string) (*tag.Tag, error) {
	tag := new(tag.Tag)
	var timestampCreated int64
	var timestampLastEdit int64

	row := m.db.QueryRow("SELECT id, ident, creatorID, guildID, content, created, lastEdit FROM tags "+
		"WHERE ident = ? AND guildID = ?", ident, guildID)

	err := row.Scan(&tag.ID, &tag.Ident, &tag.CreatorID, &tag.GuildID,
		&tag.Content, &timestampCreated, &timestampLastEdit)
	if err == sql.ErrNoRows {
		return nil, ErrDatabaseNotFound
	}
	if err != nil {
		return nil, err
	}

	tag.Created = time.Unix(timestampCreated, 0)
	tag.LastEdit = time.Unix(timestampLastEdit, 0)

	return tag, nil
}

func (m *SqliteDriver) GetGuildTags(guildID string) ([]*tag.Tag, error) {
	rows, err := m.db.Query("SELECT id, ident, creatorID, guildID, content, created, lastEdit FROM tags "+
		"WHERE guildID = ?", guildID)
	if err == sql.ErrNoRows {
		return nil, ErrDatabaseNotFound
	}
	if err != nil {
		return nil, err
	}

	tags := make([]*tag.Tag, 0)
	var timestampCreated int64
	var timestampLastEdit int64
	for rows.Next() {
		tag := new(tag.Tag)
		err = rows.Scan(&tag.ID, &tag.Ident, &tag.CreatorID, &tag.GuildID,
			&tag.Content, &timestampCreated, &timestampLastEdit)
		if err != nil {
			return nil, err
		}
		tag.Created = time.Unix(timestampCreated, 0)
		tag.LastEdit = time.Unix(timestampLastEdit, 0)
		tags = append(tags, tag)
	}

	return tags, nil
}

func (m *SqliteDriver) DeleteTag(id snowflake.ID) error {
	_, err := m.db.Exec("DELETE FROM tags WHERE id = ?", id)
	if err == sql.ErrNoRows {
		return ErrDatabaseNotFound
	}
	return err
}

func (m *SqliteDriver) SetSession(key, userID string, expires time.Time) error {
	res, err := m.db.Exec("UPDATE sessions SET sessionkey = ?, expires = ? WHERE userID = ?", key, expires, userID)
	ar, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if ar == 0 {
		_, err = m.db.Exec("INSERT INTO sessions (sessionkey, userID, expires) VALUES (?, ?, ?)", key, userID, expires)
	}
	return err
}

func (m *SqliteDriver) GetSession(key string) (string, error) {
	var userID string
	var expires time.Time
	err := m.db.QueryRow("SELECT userID, expires FROM sessions WHERE sessionkey = ?", key).
		Scan(&userID, &expires)

	if err == sql.ErrNoRows {
		return "", ErrDatabaseNotFound
	}
	if err != nil {
		return "", err
	}

	if expires.Before(time.Now()) {
		return "", ErrDatabaseNotFound
	}

	return userID, nil
}

func (m *SqliteDriver) DeleteSession(userID string) error {
	_, err := m.db.Exec("DELETE FROM sessions WHERE userID = ?", userID)
	if err == sql.ErrNoRows {
		return ErrDatabaseNotFound
	}
	return err
}

func (m *SqliteDriver) GetImageData(id snowflake.ID) (*imgstore.Image, error) {
	img := new(imgstore.Image)
	row := m.db.QueryRow("SELECT id, mimeType, data FROM imagestore WHERE id = ?", id)
	err := row.Scan(&img.ID, &img.MimeType, &img.Data)
	if err == sql.ErrNoRows {
		return nil, ErrDatabaseNotFound
	}
	if err != nil {
		return nil, err
	}

	img.Size = len(img.Data)

	return img, nil
}

func (m *SqliteDriver) SaveImageData(img *imgstore.Image) error {
	_, err := m.db.Exec("INSERT INTO imagestore (id, mimeType, data) VALUES (?, ?, ?)", img.ID, img.MimeType, img.Data)
	return err
}

func (m *SqliteDriver) RemoveImageData(id snowflake.ID) error {
	_, err := m.db.Exec("DELETE FROM imagestore WHERE id = ?", id)
	if err == sql.ErrNoRows {
		return ErrDatabaseNotFound
	}
	return err
}

func (m *SqliteDriver) SetAPIToken(token *models.APITokenEntry) (err error) {
	res, err := m.db.Exec(
		"UPDATE apitokens SET "+
			"salt = ?, created = ?, expires = ?, lastAccess = ?, hits = ? "+
			"WHERE userID = ?",
		token.Salt, token.Created, token.Expires, token.LastAccess, token.Hits, token.UserID)
	if err != nil {
		return
	}

	ar, err := res.RowsAffected()
	if err != nil {
		return
	}
	if ar == 0 {
		_, err = m.db.Exec(
			"INSERT INTO apitokens "+
				"(userID, salt, created, expires, lastAccess, hits) "+
				"VALUES (?, ?, ?, ?, ?, ?)",
			token.UserID, token.Salt, token.Created, token.Expires, token.LastAccess, token.Hits)
	}
	return
}

func (m *SqliteDriver) GetAPIToken(userID string) (t *models.APITokenEntry, err error) {
	t = new(models.APITokenEntry)
	err = m.db.QueryRow(
		"SELECT userID, salt, created, expires, lastAccess, hits "+
			"FROM apitokens WHERE userID = ?", userID).
		Scan(&t.UserID, &t.Salt, &t.Created, &t.Expires, &t.LastAccess, &t.Hits)
	if err == sql.ErrNoRows {
		err = ErrDatabaseNotFound
	}
	return
}

func (m *SqliteDriver) DeleteAPIToken(userID string) error {
	_, err := m.db.Exec("DELETE FROM apitokens WHERE userID = ?", userID)
	if err == sql.ErrNoRows {
		return ErrDatabaseNotFound
	}
	return err
}
