package main

import (
	"bytes"
	"fmt"
	"io"
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
			// Read-only image files: a Close error is neither actionable nor
			// meaningful, so it's intentionally ignored.
			_ = f.Close()
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
		contentType, reader, err := sniffContentType(file)
		if err != nil {
			closeAll()
			return nil, nil, err
		}
		files = append(files, &discordgo.File{
			ContentType: contentType,
			Name:        filename,
			Reader:      reader,
		})
	}
	return files, closeAll, nil
}

func readImage(path string) (*os.File, error) {
	return os.Open(path)
}

// sniffContentType detects the content type of r by reading its first bytes,
// then returns a reader that still yields the full, unconsumed stream. Unlike a
// seek-and-rewind, this must not assume r is seekable: r may be an *os.File
// today but an S3 GetObject body (a plain io.ReadCloser) once delivery moves
// behind the BlobStore port, so the sniffed prefix has to be stitched back on.
func sniffContentType(r io.Reader) (string, io.Reader, error) {
	buf := make([]byte, 512)
	n, err := io.ReadFull(r, buf)
	if err != nil && err != io.ErrUnexpectedEOF {
		return "", nil, fmt.Errorf("sniff: %w", err)
	}

	// detect only bytes read directly from the buffer, in case file is less than 512b
	// will not read the empty bytes
	contentType := http.DetectContentType(buf[:n])

	return contentType, io.MultiReader(bytes.NewReader(buf[:n]), r), nil
}

func getAbsolutePath(path string) (string, error) {
	wd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	return filepath.Join(wd, path), nil
}
