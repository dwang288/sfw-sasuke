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

func checkErr(err error) {
	if err != nil {
		log.Fatal(err)
	}
}

func main() {
	guildID := flag.String("guild", "", "Test guild ID. If not passed - bot registers commands globally")
	usingEnvFile := flag.Bool("use-env-file", false, "Load and use local env file. Usually used when running outside of container.")
	flag.Parse()
	if *usingEnvFile {
		err := godotenv.Load(getAbsolutePath("env/config.env"))
		checkErr(err)
	}
	// TODO: Move this into the env file conditional once we have a good way to store creds
	err := godotenv.Load(getAbsolutePath("env/secrets.env"))
	checkErr(err)

	discord, err := discordgo.New("Bot " + os.Getenv("BOT_TOKEN"))
	conf := config.New(os.Getenv("CMD_METADATA_PATH"))
	checkErr(err)

	Run(discord, conf, guildID)

}

func Run(discord *discordgo.Session, conf config.ConfigMap, guildID *string) {
	commands := buildCommands(conf)

	addHandlers(discord, buildHandlers(conf))

	err := discord.Open()
	checkErr(err)

	log.Println("Adding commands...")
	registeredCommands := make([]*discordgo.ApplicationCommand, len(commands))
	for i, v := range commands {
		cmd, err := discord.ApplicationCommandCreate(discord.State.User.ID, *guildID, v)
		checkErr(err)
		registeredCommands[i] = cmd
	}

	defer func() {
		log.Println("Removing commands...")
		// Using commands registered in earlier array, consider directly fetching them from server
		// in case we lose the list of registered commands somehow, such as with an instance shutdown
		// Also, maybe do these in parallel or batch them
		for _, v := range registeredCommands {
			err := discord.ApplicationCommandDelete(discord.State.User.ID, *guildID, v.ID)
			if err != nil {
				log.Panicf("Cannot delete '%v' command: %v", v.Name, err)
			}
		}

		discord.Close()
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt)
	log.Println("Press Ctrl+C to exit")
	<-stop

	log.Println("Gracefully shutting down.")

}
