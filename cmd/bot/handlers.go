package main

import (
	"log/slog"
	"net/http"
	"os"
	"path/filepath"

	"github.com/bwmarrin/discordgo"
	"github.com/dwang288/sfw-sasuke/pkg/config"
)

func addHandlers(discord *discordgo.Session, commandHandlers map[string]func(discord *discordgo.Session, interaction *discordgo.InteractionCreate)) {
	discord.AddHandler(func(discord *discordgo.Session, interaction *discordgo.InteractionCreate) {
		slog.Info("received interaction", "command", interaction.ApplicationCommandData().Name)
		if handler, ok := commandHandlers[interaction.ApplicationCommandData().Name]; ok {
			handler(discord, interaction)
		}
	})

	discord.AddHandler(func(discord *discordgo.Session, r *discordgo.Ready) {
		slog.Info("logged in", "username", discord.State.User.Username, "discriminator", discord.State.User.Discriminator)
	})
}

func buildCommands(conf config.ConfigMap) []*discordgo.ApplicationCommand {
	var commands []*discordgo.ApplicationCommand
	for _, v := range conf["files"] {
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
		commandHandlers[v.Name] = func(session *discordgo.Session, interaction *discordgo.InteractionCreate) {
			slog.Info("handling command", "command", v.Name, "description", v.Description, "files", v.Filenames)
			files, closeFiles, err := generateFiles(v.Filenames)
			if err != nil {
				slog.Error("failed to generate files", "command", v.Name, "error", err)
				if respErr := session.InteractionRespond(interaction.Interaction, &discordgo.InteractionResponse{
					Type: discordgo.InteractionResponseChannelMessageWithSource,
					Data: &discordgo.InteractionResponseData{
						Content: "Sorry, something went wrong serving that command.",
						Flags:   discordgo.MessageFlagsEphemeral,
					},
				}); respErr != nil {
					slog.Error("failed to send error response", "command", v.Name, "error", respErr)
				}
				return
			}
			defer closeFiles()
			if respErr := session.InteractionRespond(interaction.Interaction, &discordgo.InteractionResponse{
				Type: discordgo.InteractionResponseChannelMessageWithSource,
				Data: &discordgo.InteractionResponseData{
					Files: files,
				},
			}); respErr != nil {
				slog.Error("failed to send response", "command", v.Name, "error", respErr)
			}
		}
	}
	slog.Debug("built handlers", "count", len(commandHandlers), "keys", getKeys(commandHandlers))
	return commandHandlers
}

func getKeys(commandHandlers map[string]func(discord *discordgo.Session, interaction *discordgo.InteractionCreate)) []string {
	var keys []string
	for k := range commandHandlers {
		keys = append(keys, k)
	}
	return keys
}

func generateFiles(filenames []string) ([]*discordgo.File, func(), error) {
	var files []*discordgo.File
	var opened []*os.File
	closeAll := func() {
		for _, f := range opened {
			f.Close()
		}
	}
	for _, filename := range filenames {
		relativePath := filepath.Join(os.Getenv("ASSETS_DIR"), filename)
		absPath, err := getAbsolutePath(relativePath)
		if err != nil {
			closeAll()
			return nil, nil, err
		}
		file, err := readImage(absPath)
		if err != nil {
			closeAll()
			return nil, nil, err
		}
		opened = append(opened, file)
		contentType, err := getContentType(file)
		if err != nil {
			closeAll()
			return nil, nil, err
		}
		files = append(files, &discordgo.File{
			ContentType: contentType,
			Name:        filename,
			Reader:      file,
		})
	}
	return files, closeAll, nil
}

func readImage(path string) (*os.File, error) {
	return os.Open(path)
}

func getContentType(file *os.File) (string, error) {
	buff := make([]byte, 512)
	_, err := file.Read(buff)
	if err != nil {
		return "", err
	}
	contentType := http.DetectContentType(buff)
	file.Seek(0, 0)
	return contentType, nil
}

func getAbsolutePath(path string) (string, error) {
	wd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	return filepath.Join(wd, path), nil
}
