package main

import (
	"flag"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/bwmarrin/discordgo"
	"github.com/dwang288/sfw-sasuke/pkg/config"
	"github.com/joho/godotenv"
)

func main() {
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, nil)))

	// No pending defers at this point in the call stack, so it's safe for
	// os.Exit to be the last thing that happens here. Every function below
	// this returns its errors instead of exiting itself, so their defers
	// (e.g. Run's command cleanup) always run first.
	if err := run(); err != nil {
		slog.Error("fatal error", "error", err)
		os.Exit(1)
	}
}

func run() error {
	guildID := flag.String("guild", "", "Test guild ID. If not passed - bot registers commands globally")
	envFile := flag.String("use-env-file", "", "Load and use local env file. Usually used when running outside of container.")
	flag.Parse()
	if *envFile != "" {
		configPath, err := filepath.Abs("env/config.env")
		if err != nil {
			return err
		}
		if err := godotenv.Load(configPath); err != nil {
			return err
		}
	}
	secretsPath, err := filepath.Abs("env/secrets.env")
	if err != nil {
		return err
	}
	if err := godotenv.Load(secretsPath); err != nil {
		return err
	}

	session, err := discordgo.New("Bot " + os.Getenv("BOT_TOKEN"))
	if err != nil {
		return err
	}
	conf, err := config.New(os.Getenv("CMD_METADATA_PATH"))
	if err != nil {
		return err
	}

	return serve(session, conf, guildID)
}

// serve opens the gateway connection, creates the slash commands, and blocks
// until an interrupt/termination signal, deleting the commands on the way out.
func serve(session *discordgo.Session, conf config.Map, guildID *string) error {
	commands := buildCommands(conf)

	addHandlers(session, buildHandlers(conf))

	if err := session.Open(); err != nil {
		return err
	}

	slog.Info("creating commands")
	// A single bad command definition here should not take down the whole
	// bot or abort registration of the rest. Log and continue instead of
	// aborting, and only record commands that were actually created — the
	// deferred cleanup below dereferences every entry in
	// createdCommands, so a nil entry here would panic at shutdown.
	var createdCommands []*discordgo.ApplicationCommand
	var failed []string
	for _, v := range commands {
		cmd, err := session.ApplicationCommandCreate(session.State.User.ID, *guildID, v)
		if err != nil {
			slog.Warn("cannot create command", "command", v.Name, "error", err)
			failed = append(failed, v.Name)
			continue
		}
		createdCommands = append(createdCommands, cmd)
	}
	if len(failed) > 0 {
		slog.Warn("failed to create commands", "failed", len(failed), "total", len(commands), "commands", failed)
	} else {
		slog.Info("created commands", "count", len(createdCommands))
	}

	defer func() {
		slog.Info("deleting commands")
		var failed []string
		for _, v := range createdCommands {
			err := session.ApplicationCommandDelete(session.State.User.ID, *guildID, v.ID)
			if err != nil {
				slog.Warn("cannot delete command", "command", v.Name, "error", err)
				failed = append(failed, v.Name)
			}
		}
		if len(failed) > 0 {
			slog.Warn("failed to delete commands", "failed", len(failed), "total", len(createdCommands), "commands", failed)
		} else {
			slog.Info("deleted all commands", "count", len(createdCommands))
		}
		// Best-effort close on shutdown; a gateway close error is informational.
		_ = session.Close()
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	slog.Info("press ctrl+c to exit")
	<-stop

	slog.Info("gracefully shutting down")
	return nil
}
