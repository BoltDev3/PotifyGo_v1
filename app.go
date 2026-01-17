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
	userConfigDir, err := os.UserConfigDir()
	if err != nil {
		fmt.Printf("ERROR: Could not get user config dir: %v\n", err)
		return
	}
	a.appDir = filepath.Join(userConfigDir, "PotifyGo")
	if err := os.MkdirAll(a.appDir, 0755); err != nil {
		fmt.Printf("ERROR: Could not create app directory: %v\n", err)
	}

	a.ytdlpPath = filepath.Join(a.appDir, "yt-dlp.exe")
	a.ffmpegPath = filepath.Join(a.appDir, "ffmpeg.exe")

	tools := []string{"yt-dlp.exe", "ffmpeg.exe"}

	// Get the path of the executable
	exePath, err := os.Executable()
	if err != nil {
		fmt.Printf("ERROR: Could not get executable path: %v\n", err)
		return
	}
	baseDir := filepath.Dir(exePath)

	// Check in the "binaries" subfolder
	sourceDir := filepath.Join(baseDir, "binaries")

	for _, tool := range tools {
		target := filepath.Join(a.appDir, tool)
		source := filepath.Join(sourceDir, tool)

		// Only copy if it doesn't exist in AppData yet
		if _, err := os.Stat(target); os.IsNotExist(err) {
			if _, err := os.Stat(source); err == nil {
				input, err := os.ReadFile(source)
				if err != nil {
					fmt.Printf("ERROR: Could not read %s: %v\n", source, err)
					continue
				}
				err = os.WriteFile(target, input, 0755)
				if err != nil {
					fmt.Printf("ERROR: Could not write %s: %v\n", target, err)
					continue
				}
				fmt.Printf("MIGRATION: %s moved to AppData\n", tool)
			} else {
				fmt.Printf("ERROR: %s not found in %s\n", tool, sourceDir)
			}
		}
	}
	confPath := filepath.Join(a.appDir, "config.json")
	if _, err := os.Stat(confPath); err == nil {
		data, err := os.ReadFile(confPath)
		if err == nil {
			if err := json.Unmarshal(data, a.config); err != nil {
				fmt.Printf("ERROR: Could not unmarshal config: %v\n", err)
			} else {
				fmt.Println("Config successfully loaded.")
			}
		}
	}
}

func (a *App) logToUI(msg string) {
	if a.ctx != nil {
		runtime.EventsEmit(a.ctx, "log_event", msg)
	}
}

func (a *App) InitBranding() {
	banner := []string{
		"***************************************************",
		"* PotifyGo v1.1 - Created by BoltDev3             *",
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
	data, err := json.MarshalIndent(a.config, "", "  ")
	if err != nil {
		a.logToUI("ERROR: Could not marshal config: " + err.Error())
		return
	}
	err = os.WriteFile(filepath.Join(a.appDir, "config.json"), data, 0644)
	if err != nil {
		a.logToUI("ERROR: Could not save config: " + err.Error())
		return
	}
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
	err := filepath.WalkDir(a.config.DownloadPath, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil // Continue walking
		}
		if !d.IsDir() && strings.HasSuffix(strings.ToLower(d.Name()), ".mp3") {
			name := strings.TrimSuffix(d.Name(), filepath.Ext(d.Name()))
			files = append(files, name)
		}
		return nil
	})
	if err != nil {
		a.logToUI("ERROR: Could not scan downloaded songs: " + err.Error())
	}
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
		tok, err := auth.Token(r.Context(), state, r)
		if err != nil {
			http.Error(w, "Couldn't get token", http.StatusForbidden)
			fmt.Printf("ERROR: Couldn't get token: %v\n", err)
			return
		}
		c <- spotify.New(auth.Client(r.Context(), tok))
		fmt.Fprint(w, "Authorized! You can close this window.")
	})
	go func() {
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			fmt.Printf("ERROR: ListenAndServe: %v\n", err)
		}
	}()
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
	var res []map[string]string
	offset := 0
	for {
		p, err := a.client.CurrentUsersPlaylists(a.ctx, spotify.Limit(50), spotify.Offset(offset))
		if err != nil {
			a.logToUI("ERROR: Could not fetch playlists: " + err.Error())
			break
		}
		if len(p.Playlists) == 0 {
			break
		}
		for _, l := range p.Playlists {
			res = append(res, map[string]string{"name": l.Name, "id": string(l.ID)})
		}
		if len(res) >= int(p.Total) {
			break
		}
		offset += 50
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
			res, err := a.client.CurrentUsersTracks(a.ctx, spotify.Limit(50), spotify.Offset(offset))
			if err != nil {
				a.logToUI("ERROR: Could not fetch liked tracks: " + err.Error())
				break
			}
			if len(res.Tracks) == 0 {
				break
			}
			for _, v := range res.Tracks {
				if len(v.Artists) > 0 {
					tracks = append(tracks, v.Artists[0].Name+" - "+v.Name)
				} else {
					tracks = append(tracks, "Unknown Artist - "+v.Name)
				}
			}
			if len(tracks) >= int(res.Total) {
				break
			}
		} else {
			res, err := a.client.GetPlaylistTracks(a.ctx, spotify.ID(id), spotify.Limit(50), spotify.Offset(offset))
			if err != nil {
				a.logToUI("ERROR: Could not fetch playlist tracks: " + err.Error())
				break
			}
			if len(res.Tracks) == 0 {
				break
			}
			for _, v := range res.Tracks {
				if len(v.Track.Artists) > 0 {
					tracks = append(tracks, v.Track.Artists[0].Name+" - "+v.Track.Name)
				} else {
					tracks = append(tracks, "Unknown Artist - "+v.Track.Name)
				}
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
	err := filepath.Walk(a.config.DownloadPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil // Continue walking
		}
		if !info.IsDir() && strings.HasSuffix(strings.ToLower(info.Name()), ".mp3") {
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
					} else {
						a.logToUI("DELETE_ERROR: Could not remove file: " + err.Error())
					}
				}
			}
		}
		return nil
	})

	if err != nil {
		a.logToUI("ERROR: Error walking download path: " + err.Error())
	}

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
		"--ffmpeg-location", a.ffmpegPath,
		"--output", outputTemplate,
		"ytsearch1:"+songName)

	// --- FIX FOR BLACK WINDOW ON WINDOWS ---
	setSysProcAttr(cmd)
	// ------------------------------------------------

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		a.logToUI("ERROR: Could not get stdout pipe: " + err.Error())
		return "ERROR"
	}
	cmd.Stderr = cmd.Stdout

	if err := cmd.Start(); err != nil {
		a.logToUI("ERROR: Could not start download: " + err.Error())
		return "ERROR"
	}
	a.currentCmd = cmd

	scanner := bufio.NewScanner(stdout)
	re := regexp.MustCompile(`([\d\.]+)%`)
	for scanner.Scan() {
		line := scanner.Text()
		m := re.FindStringSubmatch(line)
		if len(m) > 1 {
			p, err := strconv.ParseFloat(m[1], 64)
			if err == nil {
				runtime.EventsEmit(a.ctx, "download_progress", map[string]interface{}{
					"song":    songName,
					"percent": int(p),
				})
			}
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
