package main

import (
	"flag"
	"io"
	"log"
	"os"
	"os/signal"
	"strings"

	"github.com/bwmarrin/discordgo"
	"github.com/joho/godotenv"
)

type config struct {
	Name        string
	Description string
	Filepaths   []string
}

func commandData() map[string]config {

	configMap := make(map[string]config)

	configMap["sfw"] = config{
		Name:        "sfw",
		Description: "cleanse the chat",
		Filepaths: []string{
			"static/sfw-sasuke-crop1.png",
			"static/sfw-sasuke-crop2.png",
			"static/sfw-sasuke-crop3.png",
			"static/sfw-sasuke-crop4.png",
		},
	}
	configMap["razzle"] = config{
		Name:        "razzle",
		Description: "hit 'em with the ol' razzle dazzle",
		Filepaths: []string{
			"static/razzle.png",
		},
	}
	return configMap
}

var (
	GuildID = flag.String("guild", "", "Test guild ID. If not passed - bot registers commands globally")
)

func checkErr(err error) {
	if err != nil {
		log.Fatal(err)
	}
}

func commandsBuilder() []*discordgo.ApplicationCommand {
	var commands []*discordgo.ApplicationCommand
	for _, v := range commandData() {
		v := v
		commands = append(commands, &discordgo.ApplicationCommand{
			Name:        v.Name,
			Description: v.Description,
		})
	}
	return commands
}

func handlersBuilder() map[string]func(discord *discordgo.Session, interaction *discordgo.InteractionCreate) {
	commandHandlers := make(map[string]func(discord *discordgo.Session, interaction *discordgo.InteractionCreate))
	for _, v := range commandData() {
		// The v I'm passing in is a pointer to a struct, therefore when the function is actually called and evaluated
		// the v will always be pointing to the last value. Reassigning the variable does a deep value copy.
		// TODO: Figure out why this didn't happen with the command array of structs
		v := v
		log.Printf("config struct: %v", v)
		commandHandlers[v.Name] = func(session *discordgo.Session, interaction *discordgo.InteractionCreate) {
			// TODO: return []Files here, then convert them into []Readers to pass into the struct
			files := filesGenerator(v.Filepaths)
			log.Printf("Name: %s, Desc: %s, Files: %v", v.Name, v.Description, v.Filepaths)

			session.InteractionRespond(interaction.Interaction, &discordgo.InteractionResponse{
				Type: discordgo.InteractionResponseChannelMessageWithSource,
				Data: &discordgo.InteractionResponseData{
					Files: files,
				},
			})
			// TODO: close []Files here
		}
	}
	log.Printf("handlers length: %d", len(commandHandlers))
	log.Printf("handlers keys: %v", getKeys(commandHandlers))
	return commandHandlers
}

func getKeys(commandHandlers map[string]func(discord *discordgo.Session, interaction *discordgo.InteractionCreate)) []string {
	var keys []string
	for k := range commandHandlers {
		keys = append(keys, k)
	}
	return keys
}

func init() {
	flag.Parse()
	err := godotenv.Load("env/config.env", "env/secrets.env")
	checkErr(err)
}

func main() {
	discord, err := discordgo.New("Bot " + os.Getenv("BOT_TOKEN"))
	checkErr(err)

	commands := commandsBuilder()
	commandHandlers := handlersBuilder()

	// Adds this function as a Handler to the session that can automatically run any command
	// that's executed, as long as the command is in the map
	discord.AddHandler(func(discord *discordgo.Session, interaction *discordgo.InteractionCreate) {
		log.Printf("command name: %s", interaction.ApplicationCommandData().Name)
		if handler, ok := commandHandlers[interaction.ApplicationCommandData().Name]; ok {
			handler(discord, interaction)
		}
	})

	discord.AddHandler(func(discord *discordgo.Session, r *discordgo.Ready) {
		log.Printf("Logged in as: %v#%v", discord.State.User.Username, discord.State.User.Discriminator)
	})

	err = discord.Open()
	checkErr(err)

	log.Println("Adding commands...")
	registeredCommands := make([]*discordgo.ApplicationCommand, len(commands))
	for i, v := range commands {
		cmd, err := discord.ApplicationCommandCreate(discord.State.User.ID, *GuildID, v)
		checkErr(err)
		registeredCommands[i] = cmd
	}

	defer discord.Close()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt)
	log.Println("Press Ctrl+C to exit")
	<-stop

	log.Println("Gracefully shutting down.")

}

func filesGenerator(paths []string) []*discordgo.File {
	var files []*discordgo.File
	for _, path := range paths {
		filename := strings.Split(path, "/")[1]
		files = append(files, &discordgo.File{
			ContentType: "image/png",
			Name:        filename,
			Reader:      readImage(path),
		})
	}
	return files
}

func readImage(path string) io.Reader {
	file, err := os.Open(path)
	checkErr(err)
	// REAL: defer file.Close()
	return file
}
