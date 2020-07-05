package inits

import (
	"fmt"

	"github.com/bwmarrin/discordgo"
	"github.com/zekroTJA/shinpuru/internal/commands"
	"github.com/zekroTJA/shinpuru/internal/core/config"
	"github.com/zekroTJA/shinpuru/internal/core/database"
	"github.com/zekroTJA/shinpuru/internal/core/storage"
	"github.com/zekroTJA/shinpuru/internal/util"
	"github.com/zekroTJA/shinpuru/internal/webserver"
)

func InitWebServer(s *discordgo.Session, db database.Database, st storage.Storage, cmdHandler *commands.CmdHandler, cfg *config.Config) (ws *webserver.WebServer) {
	if cfg.WebServer != nil && cfg.WebServer.Enabled {
		ws, err := webserver.New(db, st, s, cmdHandler, cfg)
		if err != nil {
			util.Log.Fatalf(fmt.Sprintf("Failed initializing web server: %s", err.Error()))
		}

		go ws.ListenAndServeBlocking()
		util.Log.Info(fmt.Sprintf("Web server running on address %s (%s)...", cfg.WebServer.Addr, cfg.WebServer.PublicAddr))
	}
	return
}
