package main

import (
	"flag"
	"io"
	"log"
	"os"
	"os/signal"
	"strings"

	"github.com/bwmarrin/discordgo"
)

func imagePaths() []string {
	return []string{
		"static/sfw-sasuke-crop1.png",
		"static/sfw-sasuke-crop2.png",
		"static/sfw-sasuke-crop3.png",
		"static/sfw-sasuke-crop4.png",
	}
}

// Passing in token through command line var
var (
	GuildID  = flag.String("guild", "", "Test guild ID. If not passed - bot registers commands globally")
	BotToken = flag.String("token", "", "Bot access token")
)

func checkErr(err error) {
	if err != nil {
		log.Fatal(err)
	}
}

func commandsBuilder() []*discordgo.ApplicationCommand {
	commands := []*discordgo.ApplicationCommand{
		{
			Name: "sfw",
			// All commands and options must have a description
			// Commands/options without description will fail the registration
			// of the command.
			Description: "cleanse the chat",
		},
	}
	return commands
}

func handlersBuilder() map[string]func(discord *discordgo.Session, interaction *discordgo.InteractionCreate) {
	commandHandlers := map[string]func(discord *discordgo.Session, interaction *discordgo.InteractionCreate){
		"sfw": func(discord *discordgo.Session, interaction *discordgo.InteractionCreate) {
			files := filesGenerator(imagePaths())
			discord.InteractionRespond(interaction.Interaction, &discordgo.InteractionResponse{
				Type: discordgo.InteractionResponseChannelMessageWithSource,
				Data: &discordgo.InteractionResponseData{
					Files: files,
				},
			})
			// TODO: close readers here
		},
	}
	return commandHandlers
}

func main() {
	flag.Parse()
	// TODO: read token from file
	// token := readToken()
	discord, err := discordgo.New("Bot " + *BotToken)
	checkErr(err)

	commands := commandsBuilder()
	commandHandlers := handlersBuilder()

	// Check that we have a handler for this command
	discord.AddHandler(func(discord *discordgo.Session, interaction *discordgo.InteractionCreate) {
		if h, ok := commandHandlers[interaction.ApplicationCommandData().Name]; ok {
			h(discord, interaction)
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

func testFilesGenerator() []*discordgo.File {
	return []*discordgo.File{{
		ContentType: "text/plain",
		Name:        "test.txt",
		Reader:      strings.NewReader("Hello Discord!!"),
	}}
}
