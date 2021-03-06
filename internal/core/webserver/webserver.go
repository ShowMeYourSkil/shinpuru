package webserver

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/bwmarrin/discordgo"

	"github.com/zekroTJA/shinpuru/internal/core/config"
	"github.com/zekroTJA/shinpuru/internal/core/database"
	"github.com/zekroTJA/shinpuru/internal/core/middleware"
	"github.com/zekroTJA/shinpuru/internal/core/storage"
	"github.com/zekroTJA/shinpuru/internal/util"
	"github.com/zekroTJA/shinpuru/pkg/discordoauth"
	"github.com/zekroTJA/shinpuru/pkg/lctimer"
	"github.com/zekroTJA/shinpuru/pkg/random"
	"github.com/zekroTJA/shireikan"

	routing "github.com/qiangxue/fasthttp-routing"
	"github.com/valyala/fasthttp"
)

// Error Objects
var (
	errNotFound         = errors.New("not found")
	errInvalidArguments = errors.New("invalid arguments")
	errNoAccess         = errors.New("access denied")
	errUnauthorized     = errors.New("unauthorized")
)

const (
	endpointLogInWithDC = "/_/loginwithdiscord"
	endpointAuthCB      = "/_/authorizationcallback"
)

// Static File Handlers
var (
	fileHandlerStatic = fasthttp.FS{
		Root:       "./web/dist/web",
		IndexNames: []string{"index.html"},
		Compress:   true,
	}
)

// WebServer exposes HTTP REST API endpoints to
// access shinpurus functionalities via a web app.
type WebServer struct {
	server *fasthttp.Server
	router *routing.Router

	db         database.Database
	st         storage.Storage
	rlm        *RateLimitManager
	auth       *Auth
	dcoauth    *discordoauth.DiscordOAuth
	session    *discordgo.Session
	cmdhandler shireikan.Handler
	pmw        *middleware.PermissionsMiddleware
	af         *AntiForgery

	config *config.Config

	lastAuthSecretRefresh time.Time
}

// New creates a new instance of WebServer consuming the passed
// database provider, storage provider, discordgo session, command
// handler, life cycle timer and configuration.
func New(db database.Database, st storage.Storage, s *discordgo.Session,
	cmd shireikan.Handler, lct *lctimer.LifeCycleTimer, config *config.Config, pmw *middleware.PermissionsMiddleware) (ws *WebServer, err error) {

	ws = new(WebServer)

	if !strings.HasPrefix(config.WebServer.PublicAddr, "http") {
		protocol := "http"
		if config.WebServer.TLS != nil && config.WebServer.TLS.Enabled {
			protocol += "s"
		}
		config.WebServer.PublicAddr = fmt.Sprintf("%s://%s", protocol, config.WebServer.PublicAddr)
	}

	if config.WebServer.APITokenKey == "" {
		config.WebServer.APITokenKey, err = random.GetRandBase64Str(32)
	} else if len(config.WebServer.APITokenKey) < 32 {
		err = errors.New("APITokenKey must have at leats a length of 32 characters")
	}
	if err != nil {
		return
	}

	ws.config = config
	ws.db = db
	ws.st = st
	ws.session = s
	ws.cmdhandler = cmd
	ws.pmw = pmw
	ws.rlm = NewRateLimitManager()
	ws.af = NewAntiForgery()
	ws.router = routing.New()
	ws.server = &fasthttp.Server{
		Handler: ws.router.HandleRequest,
	}

	ws.auth, err = NewAuth(db, s, lct, []byte(config.WebServer.APITokenKey))
	if err != nil {
		return
	}

	ws.dcoauth = discordoauth.NewDiscordOAuth(
		config.Discord.ClientID,
		config.Discord.ClientSecret,
		config.WebServer.PublicAddr+endpointAuthCB,
		ws.auth.LoginFailedHandler,
		ws.auth.LoginSuccessHandler,
	)

	ws.registerHandlers()

	return
}

// ListenAndServeBlocking starts the listening and serving
// loop of the web server which blocks the current goroutine.
//
// If an error is returned, the startup failed with the
// specified error.
func (ws *WebServer) ListenAndServeBlocking() error {
	tls := ws.config.WebServer.TLS

	if tls != nil && tls.Enabled {
		if tls.Cert == "" || tls.Key == "" {
			return errors.New("cert file and key file must be specified")
		}
		return ws.server.ListenAndServeTLS(ws.config.WebServer.Addr, tls.Cert, tls.Key)
	}

	return ws.server.ListenAndServe(ws.config.WebServer.Addr)
}

// registerHandlers registers all request handler for the
// request URL specified match tree.
func (ws *WebServer) registerHandlers() {
	// --------------------------------
	// AVAILABLE WITHOUT AUTH

	ws.router.Use(
		ws.addHeaders, ws.optionsHandler,
		ws.handlerFiles, ws.handleMetrics)

	imagestore := ws.router.Group("/imagestore")
	imagestore.
		Get("/<id>", ws.handlerGetImage)

	utils := ws.router.Group("/api/util")
	utils.
		Get(`/color/<hexcode:[\da-fA-F]{6,8}>`, ws.handlerGetColor)
	utils.
		Get("/commands", ws.handlerGetCommands)
	utils.
		Get("/landingpageinfo", ws.handlerGetLandingPageInfo)

	ws.router.Get("/invite", ws.handlerGetInvite)

	// --------------------------------
	// ONLY AVAILABLE AFTER AUTH

	ws.router.Get(endpointLogInWithDC, ws.dcoauth.HandlerInit)
	ws.router.Get(endpointAuthCB, ws.dcoauth.HandlerCallback)

	ws.router.Use(ws.auth.checkAuth)
	if !util.DevModeEnabled {
		ws.router.Use(ws.af.Handler)
	}

	api := ws.router.Group("/api")
	api.
		Get("/me", ws.af.SessionSetHandler, ws.handlerGetMe)
	api.
		Post("/logout", ws.auth.LogOutHandler)
	api.
		Get("/sysinfo", ws.handlerGetSystemInfo)

	settings := api.Group("/settings")
	settings.
		Get("/presence", ws.handlerGetPresence).
		Post(ws.handlerPostPresence)
	settings.
		Get("/noguildinvite", ws.handlerGetInviteSettings).
		Post(ws.handlerPostInviteSettings)

	guilds := api.Group("/guilds")
	guilds.
		Get("", ws.handlerGuildsGet)

	guild := guilds.Group("/<guildid:[0-9]+>")
	guild.
		Get("", ws.handlerGuildsGetGuild)
	guild.
		Get("/permissions", ws.handlerGetGuildPermissions).
		Post(ws.handlerPostGuildPermissions)
	guild.
		Get("/members", ws.handlerGetGuildMembers)
	guild.
		Post("/inviteblock", ws.handlerPostGuildInviteBlock)
	guild.
		Get("/scoreboard", ws.handlerGetGuildScoreboard)
	guild.
		Get("/antiraid/joinlog", ws.handlerGetGuildAntiraidJoinlog).
		Delete(ws.handlerDeleteGuildAntiraidJoinlog)

	guildUnbanRequests := guild.Group("/unbanrequests")
	guildUnbanRequests.
		Get("", ws.handlerGetGuildUnbanrequests)
	guildUnbanRequests.
		Get("/count", ws.handlerGetGuildUnbanrequestsCount)
	guildUnbanRequests.
		Get("/<id:[0-9]+>", ws.handlerGetGuildUnbanrequest).
		Post(ws.handlerPostGuildUnbanrequest)

	guildSettings := guild.Group("/settings")
	guildSettings.
		Get("/karma", ws.handlerGetGuildSettingsKarma).
		Post(ws.handlerPostGuildSettingsKarma)
	guildSettings.
		Get("/antiraid", ws.handlerGetGuildSettingsAntiraid).
		Post(ws.handlerPostGuildSettingsAntiraid)

	guild.
		Get("/settings", ws.handlerGetGuildSettings).
		Post(ws.handlerPostGuildSettings)

	guildReports := guild.Group("/reports")
	guildReports.
		Get("", ws.handlerGetReports)
	guildReports.
		Get("/count", ws.handlerGetReportsCount)

	guildBackups := guild.Group("/backups")
	guildBackups.
		Get("", ws.handlerGetGuildBackups)
	guildBackups.
		Post("/toggle", ws.handlerPostGuildBackupsToggle)
	guildBackups.
		Get("/<backupid:[0-9]+>/download", ws.handlerGetGuildBackupDownload)

	member := guilds.Group("/<guildid:[0-9]+>/<memberid:[0-9]+>")
	member.
		Get("", ws.handlerGuildsGetMember)
	member.
		Get("/permissions", ws.handlerGetMemberPermissions)
	member.
		Get("/permissions/allowed", ws.handlerGetMemberPermissionsAllowed)
	member.
		Post("/kick", ws.handlerPostGuildMemberKick)
	member.
		Post("/ban", ws.handlerPostGuildMemberBan)
	member.
		Post("/mute", ws.handlerPostGuildMemberMute)
	member.
		Post("/unmute", ws.handlerPostGuildMemberUnmute)
	member.
		Get("/unbanrequests", ws.handlerGetGuildMemberUnbanrequests)

	memberReports := member.Group("/reports")
	memberReports.
		Get("", ws.handlerGetReports).
		Post(ws.handlerPostGuildMemberReport)
	memberReports.
		Get("/count", ws.handlerGetReportsCount)

	reports := api.Group("/reports")
	report := reports.Group("/<id:[0-9]+>")
	report.
		Get("", ws.handlerGetReport)
	report.
		Post("/revoke", ws.handlerPostReportRevoke)

	unbanReqeusts := api.Group("/unbanrequests")
	unbanReqeusts.
		Get("", ws.handlerGetUnbanrequest).
		Post(ws.handlerPostUnbanrequest)
	unbanReqeusts.
		Get("/bannedguilds", ws.handlerGetUnbanrequestBannedguilds)

	api.
		Get("/token", ws.handlerGetToken).
		Post(ws.handlerPostToken).
		Delete(ws.handlerDeleteToken)
}
