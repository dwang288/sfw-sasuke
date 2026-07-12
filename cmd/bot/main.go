package main

import (
	"flag"
	"log/slog"
	"os"
	"os/signal"
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
	usingEnvFile := flag.String("use-env-file", "", "Load and use local env file. Usually used when running outside of container.")
	flag.Parse()
	if *usingEnvFile != "" {
		configPath, err := getAbsolutePath("env/config.env")
		if err != nil {
			return err
		}
		if err := godotenv.Load(configPath); err != nil {
			return err
		}
	}
	secretsPath, err := getAbsolutePath("env/secrets.env")
	if err != nil {
		return err
	}
	if err := godotenv.Load(secretsPath); err != nil {
		return err
	}

	discord, err := discordgo.New("Bot " + os.Getenv("BOT_TOKEN"))
	if err != nil {
		return err
	}
	conf, err := config.New(os.Getenv("CMD_METADATA_PATH"))
	if err != nil {
		return err
	}

	return Run(discord, conf, guildID)
}

func Run(discord *discordgo.Session, conf config.ConfigMap, guildID *string) error {
	commands := buildCommands(conf)

	addHandlers(discord, buildHandlers(conf))

	if err := discord.Open(); err != nil {
		return err
	}

	slog.Info("adding commands")
	// A single bad command definition here should not take down the whole
	// bot or abort registration of the rest. Log and continue instead of
	// aborting, and only record commands that actually registered — the
	// deferred cleanup below dereferences every entry in
	// registeredCommands, so a nil entry here would panic at shutdown.
	var registeredCommands []*discordgo.ApplicationCommand
	var failedRegistrations []string
	for _, v := range commands {
		cmd, err := discord.ApplicationCommandCreate(discord.State.User.ID, *guildID, v)
		if err != nil {
			slog.Warn("cannot create command", "command", v.Name, "error", err)
			failedRegistrations = append(failedRegistrations, v.Name)
			continue
		}
		registeredCommands = append(registeredCommands, cmd)
	}
	if len(failedRegistrations) > 0 {
		slog.Warn("failed to register commands", "failed", len(failedRegistrations), "total", len(commands), "commands", failedRegistrations)
	} else {
		slog.Info("registered commands", "count", len(registeredCommands))
	}

	defer func() {
		slog.Info("removing commands")
		var failed []string
		for _, v := range registeredCommands {
			err := discord.ApplicationCommandDelete(discord.State.User.ID, *guildID, v.ID)
			if err != nil {
				slog.Warn("cannot delete command", "command", v.Name, "error", err)
				failed = append(failed, v.Name)
			}
		}
		if len(failed) > 0 {
			slog.Warn("failed to remove commands", "failed", len(failed), "total", len(registeredCommands), "commands", failed)
		} else {
			slog.Info("removed all commands", "count", len(registeredCommands))
		}
		// Best-effort close on shutdown; a gateway close error is informational.
		_ = discord.Close()
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	slog.Info("press ctrl+c to exit")
	<-stop

	slog.Info("gracefully shutting down")
	return nil
}
