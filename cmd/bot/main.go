package main

import (
	"flag"
	"log"
	"os"
	"os/signal"

	"github.com/bwmarrin/discordgo"
	"github.com/dwang288/sfw-sasuke/pkg/config"
	"github.com/joho/godotenv"
)

func main() {
	// No pending defers at this point in the call stack, so it's safe for
	// log.Fatal (os.Exit) to be the last thing that happens here. Every
	// function below this returns its errors instead of calling log.Fatal
	// itself, so their defers (e.g. Run's command cleanup) always run first.
	if err := run(); err != nil {
		log.Fatal(err)
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

	log.Println("Adding commands...")
	// Unlike checkErr's other call sites in this function (e.g. discord.Open
	// above), a single bad command definition here should not take down the
	// whole bot or abort registration of the rest. Log and continue instead
	// of calling checkErr/log.Fatal, and only record commands that actually
	// registered — the deferred cleanup below dereferences every entry in
	// registeredCommands, so a nil entry here would panic at shutdown.
	var registeredCommands []*discordgo.ApplicationCommand
	var failedRegistrations []string
	for _, v := range commands {
		cmd, err := discord.ApplicationCommandCreate(discord.State.User.ID, *guildID, v)
		if err != nil {
			log.Printf("Cannot create '%v' command: %v", v.Name, err)
			failedRegistrations = append(failedRegistrations, v.Name)
			continue
		}
		registeredCommands = append(registeredCommands, cmd)
	}
	if len(failedRegistrations) > 0 {
		log.Printf("Failed to register %d/%d commands: %v", len(failedRegistrations), len(commands), failedRegistrations)
	} else {
		log.Printf("Registered all %d commands", len(commands))
	}

	defer func() {
		log.Println("Removing commands...")
		var failed []string
		for _, v := range registeredCommands {
			err := discord.ApplicationCommandDelete(discord.State.User.ID, *guildID, v.ID)
			if err != nil {
				log.Printf("Cannot delete '%v' command: %v", v.Name, err)
				failed = append(failed, v.Name)
			}
		}
		if len(failed) > 0 {
			log.Printf("Failed to remove %d/%d commands: %v", len(failed), len(registeredCommands), failed)
		} else {
			log.Printf("Removed all %d commands", len(registeredCommands))
		}
		discord.Close()
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt)
	log.Println("Press Ctrl+C to exit")
	<-stop

	log.Println("Gracefully shutting down.")
	return nil
}
