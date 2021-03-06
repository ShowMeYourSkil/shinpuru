package mysql

import (
	"database/sql"
	"time"

	"github.com/zekroTJA/shinpuru/internal/util"
)

const currentDbVersion = 0

type migrationFunc func(*sql.Tx) error

type migration struct {
	Version       int
	Applied       time.Time
	ReleaseTag    string
	ReleaseCommit string
}

func (m *MysqlMiddleware) Migrate() (err error) {
	mig, err := m.getLatestMigration()
	if err == sql.ErrNoRows {
		mig = &migration{
			Version: -1,
		}
	} else if err != nil {
		return
	}

	tx, err := m.Db.Begin()
	if err != nil {
		return
	}
	for i := mig.Version + 1; i < len(migrationFuncs); i++ {
		util.Log.Infof("Database: Applying migration version %d...", i)
		if err = migrationFuncs[i](tx); err != nil {
			return
		}
		if err = putMigrationVersion(tx, i); err != nil {
			return
		}
	}
	err = tx.Commit()

	return
}

func (m *MysqlMiddleware) getLatestMigration() (mig *migration, err error) {
	mig = new(migration)
	row := m.Db.QueryRow(
		`SELECT version, applied, releaseTag, releaseCommit
		FROM migrations
		ORDER BY version DESC
		LIMIT 1`)
	err = row.Scan(&mig.Version, &mig.Applied, &mig.ReleaseTag, &mig.ReleaseCommit)
	return
}

func putMigrationVersion(tx *sql.Tx, i int) (err error) {
	_, err = tx.Exec(
		`INSERT INTO migrations (version, applied, releaseTag, releaseCommit)
		VALUES (?, ?, ?, ?)`,
		i, time.Now(), util.AppVersion, util.AppCommit)
	return
}
