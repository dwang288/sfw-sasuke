package main

import (
	"log"
	"net/http"
	"os"
	"path/filepath"

	"github.com/bwmarrin/discordgo"
	"github.com/dwang288/sfw-sasuke/pkg/config"
)

func addHandlers(discord *discordgo.Session, commandHandlers map[string]func(discord *discordgo.Session, interaction *discordgo.InteractionCreate)) {
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
}
func buildCommands(conf config.ConfigMap) []*discordgo.ApplicationCommand {

	var commands []*discordgo.ApplicationCommand

	for _, v := range conf["files"] {
		v := v
		commands = append(commands, &discordgo.ApplicationCommand{
			Name:        v.Name,
			Description: v.Description,
		})
	}
	return commands
}

func buildHandlers(conf config.ConfigMap) map[string]func(discord *discordgo.Session, interaction *discordgo.InteractionCreate) {

	commandHandlers := make(map[string]func(discord *discordgo.Session, interaction *discordgo.InteractionCreate))

	for _, v := range conf["files"] {
		// The v I'm passing in is a pointer to a struct, therefore when the function is actually called and evaluated
		// the v will always be pointing to the last value. Reassigning the variable does a deep value copy.
		// TODO: Figure out why this didn't happen with the command array of structs
		v := v
		// log.Printf("config struct: %v", v)
		commandHandlers[v.Name] = func(session *discordgo.Session, interaction *discordgo.InteractionCreate) {
			// TODO: return []Files here, then convert them into []Readers to pass into the struct
			files := generateFiles(v.Filenames)
			log.Printf("Name: %s, Desc: %s, Files: %v", v.Name, v.Description, v.Filenames)

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

func generateFiles(filenames []string) []*discordgo.File {
	var files []*discordgo.File
	for _, filename := range filenames {

		relativePath := filepath.Join(os.Getenv("ASSETS_DIR"), filename)
		file := readImage(getAbsolutePath(relativePath))
		contentType := getContentType(file)

		files = append(files, &discordgo.File{
			ContentType: contentType,
			Name:        filename,
			Reader:      file,
		})
	}
	return files
}

func readImage(path string) *os.File {
	file, err := os.Open(path)
	checkErr(err)
	return file
}

func getContentType(file *os.File) string {
	buff := make([]byte, 512)
	_, err := file.Read(buff)
	checkErr(err)
	contentType := http.DetectContentType(buff)
	file.Seek(0, 0)
	return contentType
}

func getAbsolutePath(path string) string {
	execPath, err := os.Executable()
	checkErr(err)
	return filepath.Join(filepath.Dir(execPath), path)
}
