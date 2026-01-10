package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/wailsapp/wails/v2/pkg/runtime"
	"github.com/zmb3/spotify/v2"
	spotifyauth "github.com/zmb3/spotify/v2/auth"
)

type App struct {
	ctx          context.Context
	appDir       string
	config       *Config
	ytdlpPath    string
	client       *spotify.Client
	currentCmd   *exec.Cmd
	isCancelling bool
	ffmpegPath   string
}

type Config struct {
	ClientID     string `json:"client_id"`
	ClientSecret string `json:"client_secret"`
	DownloadPath string `json:"download_path"`
}

func NewApp() *App {
	return &App{config: &Config{}}
}

func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
	userConfigDir, _ := os.UserConfigDir()
	a.appDir = filepath.Join(userConfigDir, "PotifyGo")
	_ = os.MkdirAll(a.appDir, 0755)

	a.ytdlpPath = filepath.Join(a.appDir, "yt-dlp.exe")
	a.ffmpegPath = filepath.Join(a.appDir, "ffmpeg.exe")

	tools := []string{"yt-dlp.exe", "ffmpeg.exe"}

	// Wo liegt die PotifyGo.exe?
	exePath, _ := os.Executable()
	baseDir := filepath.Dir(exePath)

	// WICHTIG: Er schaut jetzt in den Unterordner "binaries"
	sourceDir := filepath.Join(baseDir, "binaries")

	for _, tool := range tools {
		target := filepath.Join(a.appDir, tool)
		source := filepath.Join(sourceDir, tool)

		// Nur kopieren, wenn es in AppData noch nicht existiert
		if _, err := os.Stat(target); os.IsNotExist(err) {
			if _, err := os.Stat(source); err == nil {
				input, _ := os.ReadFile(source)
				_ = os.WriteFile(target, input, 0755)
				fmt.Printf("MIGRATION: %s moved to AppData\n", tool)
			} else {
				fmt.Printf("ERROR: %s not found in %s\n", tool, sourceDir)
			}
		}
	}
	confPath := filepath.Join(a.appDir, "config.json")
	if _, err := os.Stat(confPath); err == nil {
		data, _ := os.ReadFile(confPath)
		_ = json.Unmarshal(data, a.config)
		// Das hier sieht man nur in der Konsole, schick es lieber auch ans UI:
		fmt.Println("Config successfully loaded.")
	}
	// Dein restlicher Config-Load Code...
}

func (a *App) logToUI(msg string) {
	if a.ctx != nil {
		runtime.EventsEmit(a.ctx, "log_event", msg)
	}
}

func (a *App) InitBranding() {
	banner := []string{
		"***************************************************",
		"* PotifyGo v1.1O - Created by bolddev3            *",
		"***************************************************",
		"SYSTEM: Ready. Config loaded: " + strconv.FormatBool(a.config.DownloadPath != ""),
	}
	for _, line := range banner {
		a.logToUI(line)
		time.Sleep(10 * time.Millisecond)
	}
}

func (a *App) GetConfig() *Config {
	return a.config
}

func (a *App) SaveConfig(cid, secret, path string) {
	a.config.ClientID = cid
	a.config.ClientSecret = secret
	a.config.DownloadPath = path
	a.internalPersist()
}

func (a *App) internalPersist() {
	data, _ := json.MarshalIndent(a.config, "", "  ")
	_ = os.WriteFile(filepath.Join(a.appDir, "config.json"), data, 0644)
	a.logToUI("SYSTEM: Configuration saved.")
}

func (a *App) SelectFolder() string {
	f, _ := runtime.OpenDirectoryDialog(a.ctx, runtime.OpenDialogOptions{})
	if f != "" {
		a.config.DownloadPath = f
		a.internalPersist()
	}
	return a.config.DownloadPath
}

func (a *App) GetDownloadedSongs() []string {
	var files []string
	if a.config.DownloadPath == "" {
		return files
	}
	_ = filepath.WalkDir(a.config.DownloadPath, func(path string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() && strings.HasSuffix(strings.ToLower(d.Name()), ".mp3") {
			name := strings.TrimSuffix(d.Name(), filepath.Ext(d.Name()))
			files = append(files, name)
		}
		return nil
	})
	return files
}

func (a *App) Login() string {
	if a.config.ClientID == "" || a.config.ClientSecret == "" {
		return "ERR"
	}
	auth := spotifyauth.New(
		spotifyauth.WithRedirectURL("http://127.0.0.1:8888/callback"),
		spotifyauth.WithScopes(spotifyauth.ScopeUserLibraryRead, spotifyauth.ScopePlaylistReadPrivate),
		spotifyauth.WithClientID(a.config.ClientID),
		spotifyauth.WithClientSecret(a.config.ClientSecret),
	)
	state := "auth_state"
	mux := http.NewServeMux()
	c := make(chan *spotify.Client)
	srv := &http.Server{Addr: "127.0.0.1:8888", Handler: mux}
	mux.HandleFunc("/callback", func(w http.ResponseWriter, r *http.Request) {
		tok, _ := auth.Token(r.Context(), state, r)
		c <- spotify.New(auth.Client(r.Context(), tok))
		fmt.Fprint(w, "Authorized!")
	})
	go srv.ListenAndServe()
	time.Sleep(200 * time.Millisecond)
	runtime.BrowserOpenURL(a.ctx, auth.AuthURL(state))
	a.client = <-c
	_ = srv.Shutdown(context.Background())
	return "SUCCESS"
}

func (a *App) GetPlaylists() []map[string]string {
	if a.client == nil {
		return nil
	}
	p, _ := a.client.CurrentUsersPlaylists(a.ctx)
	var res []map[string]string
	for _, l := range p.Playlists {
		res = append(res, map[string]string{"name": l.Name, "id": string(l.ID)})
	}
	return res
}

func (a *App) GetTracks(id string) []string {
	if a.client == nil {
		return nil
	}
	var tracks []string
	offset := 0
	for {
		if id == "liked" {
			res, _ := a.client.CurrentUsersTracks(a.ctx, spotify.Limit(50), spotify.Offset(offset))
			if res == nil || len(res.Tracks) == 0 {
				break
			}
			for _, v := range res.Tracks {
				tracks = append(tracks, v.Artists[0].Name+" - "+v.Name)
			}
			if len(tracks) >= int(res.Total) {
				break
			}
		} else {
			res, _ := a.client.GetPlaylistTracks(a.ctx, spotify.ID(id), spotify.Limit(50), spotify.Offset(offset))
			if res == nil || len(res.Tracks) == 0 {
				break
			}
			for _, v := range res.Tracks {
				tracks = append(tracks, v.Track.Artists[0].Name+" - "+v.Track.Name)
			}
			if len(tracks) >= int(res.Total) {
				break
			}
		}
		offset += 50
	}
	return tracks
}

func (a *App) DeleteTrack(song string, playlistName string) string {
	searchName := song
	if strings.Contains(song, " - ") {
		searchName = strings.Split(song, " - ")[1]
	}

	cleanSearch := strings.ToLower(regexp.MustCompile(`[^a-z0-9 ]`).ReplaceAllString(searchName, " "))
	words := strings.Fields(cleanSearch)

	if len(words) == 0 {
		words = strings.Fields(strings.ToLower(regexp.MustCompile(`[^a-z0-9 ]`).ReplaceAllString(song, " ")))
	}

	found := false
	_ = filepath.Walk(a.config.DownloadPath, func(path string, info os.FileInfo, err error) error {
		if err == nil && !info.IsDir() && strings.HasSuffix(strings.ToLower(info.Name()), ".mp3") {
			diskName := strings.ToLower(info.Name())
			matches := 0
			for _, word := range words {
				if len(word) > 2 && strings.Contains(diskName, word) {
					matches++
				}
			}

			isCorrectFile := false
			if len(words) > 0 && matches >= (len(words)+1)/2 {
				isCorrectFile = true
			}

			if isCorrectFile {
				if strings.Contains(strings.ToLower(path), strings.ToLower(a.cleanFileName(playlistName))) {
					err := os.Remove(path)
					if err == nil {
						found = true
						a.logToUI("DELETE_SUCCESS: " + info.Name())
					}
				}
			}
		}
		return nil
	})

	if found {
		return "SUCCESS"
	}
	return "NOT_FOUND"
}

func (a *App) Download(songName string, playlistName string) string {
	a.isCancelling = false
	cleanPlaylist := a.cleanFileName(playlistName)
	savePath := filepath.Join(a.config.DownloadPath, cleanPlaylist)

	err := os.MkdirAll(savePath, 0755)
	if err != nil {
		a.logToUI("ERROR: Could not create folder: " + err.Error())
		return "ERROR"
	}

	outputTemplate := filepath.Join(savePath, "%(title)s.%(ext)s")

	cmd := exec.Command(a.ytdlpPath,
		"--newline",
		"--extract-audio",
		"--audio-format", "mp3",
		"--ignore-errors",
		"--no-playlist",
		"--ffmpeg-location", filepath.Join(a.appDir, "ffmpeg.exe"),
		"--output", outputTemplate,
		"ytsearch1:"+songName)

	// --- HIER IST DER FIX FÜR DAS SCHWARZE FENSTER ---
	cmd.SysProcAttr = &syscall.SysProcAttr{
		HideWindow:    true,
		CreationFlags: 0x08000000, // CREATE_NO_WINDOW
	}
	// ------------------------------------------------

	stdout, _ := cmd.StdoutPipe()
	cmd.Stderr = cmd.Stdout
	_ = cmd.Start()
	a.currentCmd = cmd

	scanner := bufio.NewScanner(stdout)
	re := regexp.MustCompile(`([\d\.]+)%`)
	for scanner.Scan() {
		line := scanner.Text()
		m := re.FindStringSubmatch(line)
		if len(m) > 1 {
			p, _ := strconv.ParseFloat(m[1], 64)
			runtime.EventsEmit(a.ctx, "download_progress", map[string]interface{}{
				"song":    songName,
				"percent": int(p),
			})
		}
	}

	err = cmd.Wait()
	if err != nil {
		if a.isCancelling {
			return "CANCELLED"
		}
		a.logToUI("DL_ERROR for " + songName + ": " + err.Error())
		return "ERROR"
	}

	return "DONE"
}

func (a *App) CancelDownload() string {
	a.isCancelling = true
	a.logToUI("SYSTEM: Abort signal sent...")

	if a.currentCmd != nil && a.currentCmd.Process != nil {
		err := a.currentCmd.Process.Kill()
		if err != nil {
			a.logToUI("ABORT_ERROR: Could not kill process: " + err.Error())
			return "ERROR"
		}
		a.logToUI("SYSTEM: Download process terminated by user.")
	}

	return "CANCELLED"
}

func (a *App) cleanFileName(n string) string {
	re := regexp.MustCompile(`[<>:"/\\|?*]`)
	return strings.TrimSpace(re.ReplaceAllString(n, "_"))
}
