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

// commandHandler responds to a single slash command interaction.
type commandHandler func(session *discordgo.Session, interaction *discordgo.InteractionCreate)

func addHandlers(session *discordgo.Session, commandHandlers map[string]commandHandler) {
	session.AddHandler(func(session *discordgo.Session, interaction *discordgo.InteractionCreate) {
		slog.Info("received interaction", "command", interaction.ApplicationCommandData().Name)
		if handler, ok := commandHandlers[interaction.ApplicationCommandData().Name]; ok {
			handler(session, interaction)
		}
	})

	session.AddHandler(func(session *discordgo.Session, r *discordgo.Ready) {
		slog.Info("logged in", "username", session.State.User.Username, "discriminator", session.State.User.Discriminator)
	})
}

func buildCommands(conf config.Map) []*discordgo.ApplicationCommand {
	var commands []*discordgo.ApplicationCommand
	for _, entry := range conf["files"] {
		commands = append(commands, &discordgo.ApplicationCommand{
			Name:        entry.Name,
			Description: entry.Description,
		})
	}
	return commands
}

func buildHandlers(conf config.Map) map[string]commandHandler {
	commandHandlers := make(map[string]commandHandler)
	for _, entry := range conf["files"] {
		commandHandlers[entry.Name] = func(session *discordgo.Session, interaction *discordgo.InteractionCreate) {
			slog.Info("handling command", "command", entry.Name, "description", entry.Description, "files", entry.Filenames)
			files, closeFiles, err := loadFiles(entry.Filenames)
			if err != nil {
				slog.Error("failed to load files", "command", entry.Name, "error", err)
				if respErr := session.InteractionRespond(interaction.Interaction, &discordgo.InteractionResponse{
					Type: discordgo.InteractionResponseChannelMessageWithSource,
					Data: &discordgo.InteractionResponseData{
						Content: "Sorry, something went wrong serving that command.",
						Flags:   discordgo.MessageFlagsEphemeral,
					},
				}); respErr != nil {
					slog.Error("failed to send error response", "command", entry.Name, "error", respErr)
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
				slog.Error("failed to send response", "command", entry.Name, "error", respErr)
			}
		}
	}
	slog.Debug("built handlers", "count", len(commandHandlers), "keys", keys(commandHandlers))
	return commandHandlers
}

func keys(commandHandlers map[string]commandHandler) []string {
	var names []string
	for k := range commandHandlers {
		names = append(names, k)
	}
	return names
}

func loadFiles(filenames []string) ([]*discordgo.File, func(), error) {
	var files []*discordgo.File
	var opened []*os.File
	closeFiles := func() {
		for _, f := range opened {
			// Read-only image files: a Close error is neither actionable nor
			// meaningful, so it's intentionally ignored.
			_ = f.Close()
		}
	}
	for _, filename := range filenames {
		relPath := filepath.Join(os.Getenv("ASSETS_DIR"), filename)
		absPath, err := filepath.Abs(relPath)
		if err != nil {
			closeFiles()
			return nil, nil, err
		}
		file, err := openAsset(absPath)
		if err != nil {
			closeFiles()
			return nil, nil, err
		}
		opened = append(opened, file)
		contentType, reader, err := sniffContentType(file)
		if err != nil {
			closeFiles()
			return nil, nil, err
		}
		files = append(files, &discordgo.File{
			ContentType: contentType,
			Name:        filename,
			Reader:      reader,
		})
	}
	return files, closeFiles, nil
}

// openAsset is a thin seam over os.Open; it becomes the BlobStore read once
// delivery moves to object storage.
func openAsset(path string) (*os.File, error) {
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
