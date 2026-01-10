<script>
  import { onMount } from "svelte";
  import {
    GetConfig,
    SaveConfig,
    SelectFolder,
    Login,
    GetPlaylists,
    GetTracks,
    Download,
    DeleteTrack,
    GetDownloadedSongs,
    CancelDownload,
    InitBranding,
  } from "../wailsjs/go/main/App";
  import { EventsOn } from "../wailsjs/runtime/runtime";
  import { fade, fly } from "svelte/transition";

  let config = { client_id: "", client_secret: "", download_path: "" };
  let logs = [],
    songs = [],
    playlists = [],
    activeTab = "setup",
    dlStatus = {};
  let isUpdating = true,
    isDownloadingAll = false,
    showWelcome = true,
    showTutorial = false;
  let currentStep = 0,
    currentPlaylistName = "",
    toasts = [];

  let showDisclaimer = true;

  onMount(async () => {
    EventsOn("log_event", (m) => addLog(m));

    EventsOn("download_progress", (data) => {
      dlStatus[data.song] = {
        status: "loading",
        progress: data.percent,
        speed: data.speed,
      };
      dlStatus = { ...dlStatus };
    });

    const savedConfig = await GetConfig();
    if (savedConfig) {
      config = savedConfig;
      addLog(`SYSTEM: Configuration loaded. Path: ${config.download_path}`);

      if (config.download_path) {
        await refreshDownloadStatus();
      }
    }

    if (!config.client_id || !config.client_secret) showTutorial = true;

    setTimeout(() => {
      InitBranding().catch((err) => console.error("Branding Error:", err));
    }, 1000);
  });

  function addLog(msg) {
    const time = new Date().toLocaleTimeString();
    logs = [...logs, `${time} > ${msg}`].slice(-100);
    setTimeout(() => {
      const el = document.getElementById("log-stream");
      if (el) el.scrollTop = el.scrollHeight;
    }, 50);
  }

  function getBar(percent) {
    const size = 15;
    const dots = Math.floor((percent / 100) * size);
    return "[" + "#".repeat(dots) + " ".repeat(size - dots) + "]";
  }

  async function refreshDownloadStatus() {
    if (!config.download_path) return;

    const downloaded = await GetDownloadedSongs();
    let newStatus = {};

    const cleanNamesOnDisk = downloaded.map((n) =>
      n.toLowerCase().replace(/[^a-z0-9]/g, "")
    );

    songs.forEach((song) => {
      let titleOnly = song.includes(" - ") ? song.split(" - ")[1] : song;
      const cleanTitle = titleOnly.toLowerCase().replace(/[^a-z0-9]/g, "");
      const cleanFullSong = song.toLowerCase().replace(/[^a-z0-9]/g, "");

      const isFound = cleanNamesOnDisk.some((diskName) => {
        return (
          diskName.includes(cleanTitle) ||
          cleanTitle.includes(diskName) ||
          diskName.includes(cleanFullSong)
        );
      });

      if (isFound) {
        newStatus[song] = "done";
      }
    });

    dlStatus = newStatus;
  }

  async function sync(id, name) {
    currentPlaylistName = name;
    addLog(`SYNC_START: Loading playlist "${name.toUpperCase()}"`);
    songs = await GetTracks(id);
    addLog(`SYNC_END: Found ${songs.length} tracks.`);
    await refreshDownloadStatus();
    addToast(`SYNCED: ${name.toUpperCase()}`, "success");
  }

  async function handleSave() {
    await SaveConfig(
      config.client_id,
      config.client_secret,
      config.download_path
    );
    addLog("SYSTEM: Config updated and saved to disk.");
    addToast("SETTINGS SAVED", "success");
  }

  async function handleLogin() {
    if (!config.client_id || !config.client_secret)
      return addToast("SAVE CREDENTIALS FIRST!", "error");
    addLog("AUTH: Requesting Spotify access...");
    const res = await Login();
    if (res === "SUCCESS") {
      addLog("AUTH: Access granted.");
      addToast("SYSTEM AUTHORIZED", "success");
      playlists = await GetPlaylists();
      activeTab = "library";
    }
  }

  async function startDl(s) {
    if (dlStatus[s] === "done") return;
    addLog(`DL_INIT: Preparing "${s}"`);
    dlStatus[s] = { status: "loading", progress: 0, speed: "..." };
    dlStatus = { ...dlStatus };

    const res = await Download(s, currentPlaylistName);
    if (res === "DONE") {
      addLog(`DL_SUCCESS: Finished "${s}"`);
      dlStatus[s] = "done";
      await refreshDownloadStatus();
    } else {
      addLog(`DL_FAILED: Error downloading "${s}"`);
      delete dlStatus[s];
    }
    dlStatus = { ...dlStatus };
  }

  async function downloadPlaylist() {
    if (isDownloadingAll) {
      isDownloadingAll = false;
      addLog("DL_ABORT: User cancelled.");
      await CancelDownload();
      return;
    }
    const toDl = songs.filter((s) => dlStatus[s] !== "done");
    addLog(`BATCH_START: Downloading ${toDl.length} tracks.`);
    isDownloadingAll = true;
    for (const song of toDl) {
      if (!isDownloadingAll) break;
      await startDl(song);
    }
    isDownloadingAll = false;
    addLog("BATCH_END: All tasks finished.");
  }

  async function handleDelete(songName) {
    if (confirm(`Delete ${songName}?`)) {
      const res = await DeleteTrack(songName, currentPlaylistName);
      if (res === "SUCCESS") {
        addLog(`FS_REMOVAL: Deleted "${songName}"`);
        delete dlStatus[songName];
        dlStatus = { ...dlStatus };
        addToast("FILE DELETED", "info");
        await refreshDownloadStatus();
      }
    }
  }

  function addToast(msg, type = "info") {
    const id = Date.now();
    toasts = [...toasts, { id, msg, type }];
    setTimeout(() => (toasts = toasts.filter((t) => t.id !== id)), 3000);
  }

  function handleMouseWheel(e) {
    e.currentTarget.scrollLeft += e.deltaY;
    e.preventDefault();
  }

  const nextStep = () =>
    currentStep < steps.length - 1 ? currentStep++ : (showTutorial = false);

  const steps = [
    {
      t: "Step 1: Spotify Dashboard",
      d: "Create a new App at developer.spotify.com.",
    },
    {
      t: "Step 2: Redirect URI",
      d: "Add 'http://127.0.0.1:8888/callback' to your Redirect URIs.",
    },
    {
      t: "Step 3: Get Credentials",
      d: "Copy Client ID and Secret into the fields.",
    },
    { t: "Step 4: Save & Start", d: "Click 'Save' and 'Authorize'!" },
  ];
</script>

<main>
  {#if showDisclaimer}
    <div class="disclaimer-overlay" transition:fade>
      <div class="glitch-card" transition:fly={{ y: 50, duration: 500 }}>
        <div class="scanline"></div>
        <div class="hazard-stripe"></div>

        <div class="disclaimer-content">
          <div class="warning-symbol">⚠️</div>
          <h1 class="glitch-text" data-text="SYSTEM CRITICAL">Disclaimer</h1>
          <div class="version-tag">POTIFY GO // BETA_v1.1</div>

          <div class="warning-box">
            <p>
              This software is in <span class="highlight">EARLY BETA</span>.
              Expect chaotic filenames, potential performance drops, and the
              occasional logic ghost. But it works
            </p>
            <div class="status-lines">
              <span>> DELETION_LOGS: UNSTABLE</span>
              <span>> THREAD_LOAD: HIGH</span>
            </div>
          </div>

          <button class="chaos-btn" on:click={() => (showDisclaimer = false)}>
            <span class="btn-glitch"></span>
            Accept Bugs and go on.
          </button>
        </div>

        <div class="hazard-stripe bottom"></div>
      </div>
    </div>
  {/if}

  <div class="toast-container">
    {#each toasts as t (t.id)}
      <div class="toast {t.type}" in:fly={{ x: 200, duration: 300 }} out:fade>
        {t.msg}
      </div>
    {/each}
  </div>

  {#if showWelcome}
    <div class="welcome-overlay" transition:fade>
      <div class="welcome-card">
        <div class="welcome-icon">🚀</div>
        <h1>Welcome to PotifyGO</h1>
        <p>Your library, synced and offline. Fast & Clean.</p>
        <div class="features-preview">
          <div class="f-item">
            <span>✔</span> <b>Auto-Detect:</b> Skips existing files
          </div>
          <div class="f-item">
            <span>✔</span> <b>High-Speed:</b> Powered by Go & yt-dlp
          </div>
          <div class="f-item">
            <span>✔</span> <b>Bold Design:</b> Made by bolddev3
          </div>
        </div>
        <button class="start-btn" on:click={() => (showWelcome = false)}
          >Let's Get Started</button
        >
      </div>
    </div>
  {/if}

  {#if showTutorial}
    <div class="tut-overlay" transition:fade>
      <div class="tut-card">
        <div class="tut-header">
          <span>SYSTEM_GUIDE v1.0.42_PRO</span>
          <button on:click={() => (showTutorial = false)}>✕</button>
        </div>
        <div class="tut-body">
          <div class="step-indicator">STEP_0{currentStep + 1}</div>
          <h3>{steps[currentStep].t}</h3>
          <p>{steps[currentStep].d}</p>
        </div>
        <div class="tut-footer">
          <div class="prog-bg">
            <div
              class="prog-fill"
              style="width: {(currentStep + 1) * 25}%"
            ></div>
          </div>
          <button class="next-btn" on:click={nextStep}
            >{currentStep === steps.length - 1 ? "FINISH" : "NEXT STEP"}</button
          >
        </div>
      </div>
    </div>
  {/if}

  <aside class="sidebar">
    <div class="brand">POTIFY<span>.GO</span></div>
    <div class="credits">
      BUILD_BY_BOLTDEV3
      <a
        href="https://github.com/BoltDev3/PotifyGo"
        target="_blank"
        class="repo-link">GET_REPO</a
      >
    </div>
    <nav>
      <button
        class:active={activeTab === "setup"}
        on:click={() => (activeTab = "setup")}
      >
        <svg
          width="18"
          height="18"
          viewBox="0 0 24 24"
          fill="none"
          stroke="currentColor"
          stroke-width="2"
          ><path
            d="M12.22 2h-.44a2 2 0 0 0-2 2v.18a2 2 0 0 1-1 1.73l-.43.25a2 2 0 0 1-2 0l-.15-.08a2 2 0 0 0-2.73.73l-.22.38a2 2 0 0 0 .73 2.73l.15.1a2 2 0 0 1 1 1.72v.51a2 2 0 0 1-1 1.74l-.15.09a2 2 0 0 0-.73 2.73l.22.38a2 2 0 0 0 2.73.73l.15-.08a2 2 0 0 1 2 0l.43.25a2 2 0 0 1 1 1.73V20a2 2 0 0 0 2 2h.44a2 2 0 0 0 2-2v-.18a2 2 0 0 1 1-1.73l.43-.25a2 2 0 0 1 2 0l.15.08a2 2 0 0 0 2.73-.73l.22-.39a2 2 0 0 0-.73-2.73l-.15-.08a2 2 0 0 1-1-1.74v-.5a2 2 0 0 1 1-1.74l.15-.09a2 2 0 0 0 .73-2.73l-.22-.38a2 2 0 0 0-2.73-.73l-.15.08a2 2 0 0 1-2 0l-.43-.25a2 2 0 0 1-1-1.73V4a2 2 0 0 0-2-2z"
          /><circle cx="12" cy="12" r="3" /></svg
        > SETUP
      </button>
      <button
        class:active={activeTab === "library"}
        on:click={() => (activeTab = "library")}
      >
        <svg
          width="18"
          height="18"
          viewBox="0 0 24 24"
          fill="none"
          stroke="currentColor"
          stroke-width="2"
          ><path d="M9 18V5l12-2v13" /><circle cx="6" cy="18" r="3" /><circle
            cx="18"
            cy="16"
            r="3"
          /></svg
        > LIBRARY
      </button>
      <button
        class="help-btn"
        on:click={() => {
          currentStep = 0;
          showTutorial = true;
        }}>HELP</button
      >
    </nav>
  </aside>

  <section class="content">
    {#if activeTab === "setup"}
      <div class="card" in:fade={{ duration: 200 }}>
        {#if isUpdating}
          <div class="warning-banner">
            <svg
              width="45"
              height="45"
              viewBox="0 0 24 24"
              fill="none"
              stroke="currentColor"
              stroke-width="2.5"
              ><path
                d="M10.29 3.86L1.82 18a2 2 0 0 0 1.71 3h16.94a2 2 0 0 0 1.71-3L13.71 3.86a2 2 0 0 0-3.42 0z"
              /><line x1="12" y1="9" x2="12" y2="13" /><circle
                cx="12"
                cy="17"
                r="0.5"
                fill="currentColor"
              /></svg
            >
            <span
              >MAINTENANCE: Spotify Update. Please use existing App or wait.</span
            >
          </div>
        {/if}
        <h2>Configuration</h2>
        <div class="field">
          <label for="client_id">SPOTIFY_CLIENT_ID</label>
          <input id="client_id" type="password" bind:value={config.client_id} />
        </div>
        <div class="field">
          <label for="client_secret">SPOTIFY_CLIENT_SECRET</label>
          <input
            id="client_secret"
            type="password"
            bind:value={config.client_secret}
          />
        </div>
        <div class="field">
          <label for="dl_path">DOWNLOAD_PATH</label>
          <div class="row">
            <input
              id="dl_path"
              type="text"
              bind:value={config.download_path}
              readonly
            />
            <button
              class="dir"
              on:click={async () =>
                (config.download_path = await SelectFolder())}
            >
              <svg
                width="16"
                height="16"
                viewBox="0 0 24 24"
                fill="none"
                stroke="#fff"
                stroke-width="2"
                ><path
                  d="M4 20h16a2 2 0 0 0 2-2V8a2 2 0 0 0-2-2h-7.93a2 2 0 0 1-1.66-.9l-.82-1.2A2 2 0 0 0 7.93 3H4a2 2 0 0 0-2 2v13a2 2 0 0 0 2 2Z"
                /></svg
              >
            </button>
          </div>
        </div>
        <button class="save" on:click={handleSave}>SAVE SETTINGS</button>
        <button class="login" on:click={handleLogin}>AUTHORIZE SYSTEM</button>
      </div>
    {/if}

    {#if activeTab === "library"}
      <div class="lib" in:fade={{ duration: 200 }}>
        <div class="top-bar" on:wheel={handleMouseWheel}>
          <button
            class="pill liked"
            on:click={() => sync("liked", "Liked Songs")}>❤️ LIKED SONGS</button
          >
          {#each playlists as p}
            <button class="pill" on:click={() => sync(p.id, p.name)}
              >{p.name}</button
            >
          {/each}
        </div>

        {#if songs.length > 0}
          <div class="playlist-action-bar">
            <h3>{currentPlaylistName}</h3>
            <button
              class="dl-playlist-btn"
              class:cancel={isDownloadingAll}
              on:click={downloadPlaylist}
            >
              {#if isDownloadingAll}
                <svg
                  width="16"
                  height="16"
                  viewBox="0 0 24 24"
                  fill="none"
                  stroke="currentColor"
                  stroke-width="3"
                  ><rect x="6" y="6" width="12" height="12" /></svg
                > CANCEL
              {:else}
                <svg
                  width="16"
                  height="16"
                  viewBox="0 0 24 24"
                  fill="none"
                  stroke="currentColor"
                  stroke-width="3"
                  ><path
                    d="M21 15v4a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-4"
                  /><polyline points="7 10 12 15 17 10" /><line
                    x1="12"
                    x2="12"
                    y1="15"
                    y2="3"
                  /></svg
                > DOWNLOAD ALL
              {/if}
            </button>
          </div>
        {/if}

        <div class="list">
          {#each songs as s}
            <div class="item">
              <div style="display:flex; flex-direction:column; gap:4px">
                <span style="font-size:11px">{s}</span>
                {#if dlStatus[s]?.status === "loading"}
                  <span
                    style="color:#1db954; font-size:9px; font-family:monospace"
                  >
                    {getBar(dlStatus[s].progress)}
                    {dlStatus[s].progress}% @ {dlStatus[s].speed}
                  </span>
                {/if}
              </div>
              <div class="row">
                {#if dlStatus[s] === "done"}
                  <button class="btn-delete" on:click={() => handleDelete(s)}>
                    <svg
                      width="14"
                      height="14"
                      viewBox="0 0 24 24"
                      fill="none"
                      stroke="currentColor"
                      stroke-width="2"
                      ><path
                        d="M3 6h18m-2 0v14c0 1-1 2-2 2H7c-1 0-2-1-2-2V6m3 0V4c0-1 1-2 2-2h4c1 0 2 1 2 2v2"
                      /></svg
                    >
                  </button>
                {/if}
                <button
                  class="dl"
                  on:click={() => startDl(s)}
                  disabled={dlStatus[s] === "done"}
                >
                  {#if dlStatus[s]?.status === "loading"}
                    <svg
                      class="spin"
                      width="16"
                      height="16"
                      viewBox="0 0 24 24"
                      fill="none"
                      stroke="#000"
                      stroke-width="3"
                      ><path d="M21 12a9 9 0 1 1-6.21-8.56" /></svg
                    >
                  {:else if dlStatus[s] === "done"}
                    <svg
                      width="16"
                      height="16"
                      viewBox="0 0 24 24"
                      fill="none"
                      stroke="#000"
                      stroke-width="3"><polyline points="20 6 9 17 4 12" /></svg
                    >
                  {:else}
                    <svg
                      width="16"
                      height="16"
                      viewBox="0 0 24 24"
                      fill="none"
                      stroke="#000"
                      stroke-width="3"
                      ><path
                        d="M21 15v4a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-4"
                      /><polyline points="7 10 12 15 17 10" /><line
                        x1="12"
                        x2="12"
                        y1="15"
                        y2="3"
                      /></svg
                    >
                  {/if}
                </button>
              </div>
            </div>
          {/each}
        </div>
      </div>
    {/if}
  </section>

  <aside class="terminal">
    <div class="t-head">SYSTEM_LOG</div>
    <div id="log-stream" class="t-body">
      {#each logs as log}<div class="t-line"><code>></code> {log}</div>{/each}
    </div>
  </aside>
</main>

<style>
  /* DEIN CSS BLEIBT KOMPLETT ERHALTEN */
  :global(body) {
    margin: 0;
    background: #000;
    color: #fff;
    font-family: "JetBrains Mono", monospace;
    overflow: hidden;
  }
  main {
    display: flex;
    height: 100vh;
    position: relative;
  }
  .toast-container {
    position: fixed;
    top: 20px;
    right: 20px;
    z-index: 20000;
    display: flex;
    flex-direction: column;
    gap: 10px;
  }
  .toast {
    padding: 12px 20px;
    background: #080808;
    border-left: 4px solid #1db954;
    color: #fff;
    font-size: 11px;
    font-weight: bold;
    border-radius: 4px;
    box-shadow: 0 5px 15px rgba(0, 0, 0, 0.5);
    min-width: 200px;
    border: 1px solid #111;
  }
  .toast.error {
    border-left-color: #ff4444;
  }
  .toast.success {
    border-left-color: #1db954;
  }
  .welcome-overlay {
    position: fixed;
    top: 0;
    left: 0;
    width: 100%;
    height: 100%;
    background: rgba(0, 0, 0, 0.9);
    z-index: 10000;
    display: flex;
    align-items: center;
    justify-content: center;
    backdrop-filter: blur(10px);
  }
  .welcome-card {
    background: #080808;
    border: 2px solid #1db954;
    width: 420px;
    padding: 40px;
    border-radius: 24px;
    text-align: center;
  }
  .welcome-icon {
    font-size: 60px;
    margin-bottom: 20px;
  }
  .features-preview {
    margin: 30px 0;
    text-align: left;
    background: #111;
    padding: 20px;
    border-radius: 12px;
  }
  .f-item {
    margin: 10px 0;
    font-size: 13px;
  }
  .f-item span {
    color: #1db954;
    margin-right: 10px;
    font-weight: bold;
  }
  .start-btn {
    width: 100%;
    background: #1db954;
    color: #000;
    border: none;
    padding: 15px;
    font-weight: 900;
    border-radius: 30px;
    cursor: pointer;
  }
  .tut-overlay {
    position: absolute;
    top: 0;
    left: 0;
    width: 100%;
    height: 100%;
    background: rgba(0, 0, 0, 0.85);
    backdrop-filter: blur(5px);
    z-index: 999;
    display: flex;
    align-items: center;
    justify-content: center;
  }
  .tut-card {
    background: #080808;
    border: 1px solid #1db954;
    width: 400px;
    padding: 30px;
    border-radius: 12px;
  }
  .tut-header {
    display: flex;
    justify-content: space-between;
    font-size: 10px;
    color: #1db954;
    margin-bottom: 20px;
  }
  .tut-header button {
    background: none;
    border: none;
    color: #444;
    cursor: pointer;
  }
  .step-indicator {
    font-size: 30px;
    font-weight: 900;
    color: #111;
    margin-bottom: 10px;
  }
  .tut-body p {
    color: #666;
    font-size: 12px;
    line-height: 1.5;
    margin-bottom: 30px;
  }
  .prog-bg {
    background: #111;
    height: 4px;
    border-radius: 2px;
    margin-bottom: 20px;
  }
  .prog-fill {
    background: #1db954;
    height: 100%;
    transition: width 0.3s;
  }
  .next-btn {
    width: 100%;
    background: #1db954;
    color: #000;
    border: none;
    padding: 12px;
    font-weight: bold;
    border-radius: 4px;
    cursor: pointer;
  }
  .sidebar {
    width: 160px;
    background: #050505;
    border-right: 1px solid #111;
    padding: 25px;
    display: flex;
    flex-direction: column;
  }
  .brand {
    font-size: 18px;
    font-weight: 900;
    color: #1db954;
    margin-bottom: 40px;
  }
  .brand span {
    color: #fff;
  }
  nav button {
    background: none;
    border: none;
    color: #444;
    width: 100%;
    text-align: left;
    padding: 12px 0;
    cursor: pointer;
    display: flex;
    align-items: center;
    gap: 10px;
    font-weight: bold;
    font-size: 11px;
  }
  nav button.active {
    color: #1db954;
  }
  .help-btn {
    margin-top: auto;
    color: #1db954 !important;
    border: 1px solid #1db954 !important;
    justify-content: center !important;
    border-radius: 4px;
  }
  .content {
    flex: 1;
    display: flex;
    justify-content: center;
    align-items: center;
    padding: 40px;
    overflow-y: auto;
    scrollbar-width: none;
  }
  .card {
    width: 100%;
    max-width: 400px;
    background: #080808;
    padding: 30px;
    border-radius: 12px;
    border: 1px solid #111;
  }
  .warning-banner {
    background: #ff4444;
    color: #000;
    padding: 20px;
    border-radius: 8px;
    margin-bottom: 25px;
    font-size: 14px;
    font-weight: 900;
    display: flex;
    align-items: center;
    gap: 15px;
    animation: pulse 2s infinite;
  }
  .field {
    margin-bottom: 15px;
  }
  label {
    font-size: 9px;
    color: #333;
    display: block;
    margin-bottom: 5px;
  }
  input {
    width: 100%;
    background: #000;
    border: 1px solid #111;
    color: #fff;
    padding: 10px;
    border-radius: 4px;
    font-size: 11px;
  }
  .row {
    display: flex;
    gap: 5px;
    align-items: center;
  }
  .dir {
    background: #111;
    border: 1px solid #222;
    padding: 0 10px;
    cursor: pointer;
    border-radius: 4px;
    height: 32px;
  }
  .save {
    width: 100%;
    background: #111;
    color: #555;
    border: 1px solid #222;
    padding: 10px;
    margin-top: 10px;
    cursor: pointer;
    font-weight: bold;
    border-radius: 4px;
  }
  .login {
    width: 100%;
    background: #1db954;
    border: none;
    padding: 12px;
    margin-top: 10px;
    border-radius: 25px;
    font-weight: bold;
    cursor: pointer;
    color: #000;
  }
  .lib {
    width: 100%;
    max-width: 750px;
    align-self: flex-start;
  }
  .top-bar {
    display: flex;
    gap: 8px;
    overflow-x: auto;
    padding-bottom: 15px;
    border-bottom: 1px solid #111;
    margin-bottom: 20px;
  }
  .playlist-action-bar {
    display: flex;
    justify-content: space-between;
    align-items: center;
    margin-bottom: 20px;
  }
  .playlist-action-bar h3 {
    font-size: 14px;
    color: #1db954;
    margin: 0;
  }
  .dl-playlist-btn {
    background: #1db954;
    color: #000;
    border: none;
    padding: 8px 15px;
    border-radius: 20px;
    font-weight: 900;
    font-size: 10px;
    cursor: pointer;
    display: flex;
    align-items: center;
    gap: 8px;
  }
  .dl-playlist-btn.cancel {
    background: #ff4444;
    color: #fff;
  }
  .pill {
    background: #0c0c0c;
    border: 1px solid #111;
    color: #444;
    padding: 6px 14px;
    border-radius: 15px;
    cursor: pointer;
    font-size: 10px;
    white-space: nowrap;
  }
  .pill.liked {
    border-color: #1db954;
    color: #1db954;
  }
  .item {
    background: #080808;
    padding: 12px 18px;
    border-radius: 8px;
    border: 1px solid #111;
    display: flex;
    justify-content: space-between;
    align-items: center;
    margin-bottom: 8px;
  }
  .btn-delete {
    background: none;
    border: 1px solid #222;
    color: #444;
    border-radius: 50%;
    width: 32px;
    height: 32px;
    display: flex;
    align-items: center;
    justify-content: center;
    cursor: pointer;
  }
  .btn-delete:hover {
    color: #ff4444;
    border-color: #ff4444;
  }
  .dl {
    background: #1db954;
    border: none;
    border-radius: 50%;
    width: 32px;
    height: 32px;
    cursor: pointer;
    display: flex;
    align-items: center;
    justify-content: center;
  }
  .dl:disabled {
    background: #111;
  }
  .terminal {
    width: 280px;
    background: #020202;
    border-left: 1px solid #111;
    display: flex;
    flex-direction: column;
  }
  .t-head {
    padding: 15px;
    font-size: 10px;
    color: #1db954;
    font-weight: bold;
    border-bottom: 1px solid #111;
  }
  .t-body {
    flex: 1;
    padding: 15px;
    overflow-y: auto;
    font-size: 10px;
    color: #1db954;
  }
  .t-line {
    margin-bottom: 4px;
  }
  .spin {
    animation: rotation 1s infinite linear;
  }

  .credits {
    font-size: 8px;
    color: #333;
    margin-top: -35px;
    margin-bottom: 30px;
    letter-spacing: 1px;
    display: flex;
    flex-direction: column;
    gap: 4px;
  }
  .repo-link {
    color: #1db954;
    text-decoration: none;
    font-weight: bold;
    opacity: 0.6;
    transition: opacity 0.2s;
  }
  .repo-link:hover {
    opacity: 1;
    text-decoration: underline;
  }

  @keyframes rotation {
    from {
      transform: rotate(0deg);
    }
    to {
      transform: rotate(359deg);
    }
  }
  @keyframes pulse {
    0% {
      transform: scale(1);
    }
    50% {
      transform: scale(1.02);
    }
    100% {
      transform: scale(1);
    }
  }
  :global(::-webkit-scrollbar) {
    width: 6px !important;
    height: 6px !important;
  }
  :global(::-webkit-scrollbar-track) {
    background: #000 !important;
  }
  :global(::-webkit-scrollbar-thumb) {
    background: #1db954 !important;
    border-radius: 10px !important;
  }
  :global(*) {
    scrollbar-width: thin !important;
    scrollbar-color: #1db954 #000 !important;
  }

  :root {
    --bg-black: #050505;
    --panel-dark: #0a0a0a;
    --spotify-green: #1db954;
    --text-gray: #b3b3b3;
    --text-white: #ffffff;
    --border-dim: #1a1a1a;
    --terminal-green: #00ff41;
  }

  body {
    background-color: var(--bg-black);
    color: var(--text-white);
    font-family: "Segoe UI", Roboto, Helvetica, Arial, sans-serif;
    margin: 0;
  }

  /* Sidebar & Navigation */
  .sidebar {
    width: 250px;
    background-color: #000;
    padding: 20px;
    border-right: 1px solid var(--border-dim);
  }

  .nav-item {
    color: var(--text-gray);
    text-transform: uppercase;
    font-weight: bold;
    font-size: 0.8rem;
    margin: 15px 0;
    cursor: pointer;
    display: flex;
    align-items: center;
    gap: 10px;
  }

  .nav-item.active,
  .nav-item:hover {
    color: var(--spotify-green);
  }

  /* Song-Liste Cards */
  .song-card {
    background-color: var(--panel-dark);
    border-radius: 8px;
    padding: 15px 20px;
    margin-bottom: 10px;
    display: flex;
    justify-content: space-between;
    align-items: center;
    border: 1px solid transparent;
    transition: border 0.2s;
  }

  .song-card:hover {
    border-color: #333;
  }

  /* Die knalligen Buttons */
  .btn-download-all {
    background-color: var(--spotify-green);
    color: black;
    font-weight: bold;
    border-radius: 20px;
    padding: 8px 20px;
    border: none;
    cursor: pointer;
    text-transform: uppercase;
  }

  .btn-icon {
    background-color: var(--spotify-green);
    color: black;
    border-radius: 50%;
    width: 35px;
    height: 35px;
    display: flex;
    align-items: center;
    justify-content: center;
  }

  /* Der Log-Bereich auf der rechten Seite */
  .system-log {
    background-color: #000;
    border-left: 1px solid var(--border-dim);
    font-family: "Consolas", "Courier New", monospace;
    font-size: 0.75rem;
    padding: 10px;
    color: var(--terminal-green);
    overflow-y: auto;
  }

  .log-entry {
    margin-bottom: 4px;
    line-height: 1.2;
  }

  .log-timestamp {
    color: #555;
    margin-right: 8px;
  }

  /* Warning/Beta Screen */
  .beta-overlay {
    text-align: center;
    max-width: 600px;
    margin: 100px auto;
  }

  .warning-icon {
    color: #f1c40f;
    font-size: 2rem;
  }

  .btn-chaos {
    background: white;
    color: black;
    padding: 10px 20px;
    border: none;
    font-weight: bold;
    margin-top: 20px;
    cursor: pointer;
  }

  .sidebar {
    display: flex;
    flex-direction: column;
    gap: 15px;
  }

  .song-card {
    background: #0a0a0a;
    border: 1px solid #1a1a1a;
    border-radius: 6px;
    padding: 12px;
    margin-bottom: 8px;
    display: flex;
    justify-content: space-between;
    align-items: center;
  }

  .system-log {
    background: #000;
    color: #00ff41;
    font-family: "Courier New", monospace;
    font-size: 0.7rem;
    line-height: 1.4;
  }

  .btn-icon {
    background: #1db954;
    border: none;
    border-radius: 50%;
    width: 30px;
    height: 30px;
    cursor: pointer;
    display: flex;
    align-items: center;
    justify-content: center;
    color: #000;
  }

  .btn-download-all {
    background: #1db954;
    color: #000;
    font-weight: 900;
    padding: 8px 16px;
    border-radius: 20px;
    border: none;
    cursor: pointer;
  }

  /* --- NEW CYBER-DISCLAIMER STYLE --- */
  .disclaimer-overlay {
    position: fixed;
    inset: 0;
    z-index: 30000;
    background: radial-gradient(circle, rgba(10, 10, 10, 0.9) 0%, #000 100%);
    backdrop-filter: blur(15px);
    display: flex;
    align-items: center;
    justify-content: center;
    padding: 20px;
  }

  .glitch-card {
    position: relative;
    background: #080808;
    border: 1px solid #333;
    width: 100%;
    max-width: 450px;
    overflow: hidden;
    box-shadow:
      0 0 50px rgba(0, 0, 0, 1),
      0 0 20px rgba(29, 185, 84, 0.1);
  }

  .disclaimer-content {
    padding: 40px;
    text-align: center;
  }

  .hazard-stripe {
    height: 10px;
    background: repeating-linear-gradient(
      45deg,
      #f1c40f,
      #f1c40f 10px,
      #000 10px,
      #000 20px
    );
  }

  .warning-symbol {
    font-size: 50px;
    margin-bottom: 10px;
    filter: drop-shadow(0 0 10px #f1c40f);
  }

  .glitch-text {
    font-size: 2rem;
    font-weight: 900;
    color: #fff;
    letter-spacing: -1px;
    margin: 0;
    position: relative;
  }

  .version-tag {
    font-size: 10px;
    color: #1db954;
    font-family: monospace;
    margin-bottom: 30px;
    letter-spacing: 2px;
  }

  .warning-box {
    background: #0c0c0c;
    border: 1px solid #1a1a1a;
    padding: 20px;
    text-align: left;
    margin-bottom: 30px;
  }

  .warning-box p {
    font-size: 13px;
    line-height: 1.6;
    color: #888;
    margin: 0 0 15px 0;
  }

  .highlight {
    color: #f1c40f;
    font-weight: bold;
  }

  .status-lines {
    font-family: monospace;
    font-size: 9px;
    color: #444;
    display: flex;
    flex-direction: column;
  }

  .chaos-btn {
    width: 100%;
    background: transparent;
    border: 1px solid #fff;
    color: #fff;
    padding: 15px;
    font-weight: 900;
    text-transform: uppercase;
    cursor: pointer;
    position: relative;
    transition: all 0.2s;
  }

  .chaos-btn:hover {
    background: #fff;
    color: #000;
    box-shadow: 0 0 20px rgba(255, 255, 255, 0.4);
  }

  .scanline {
    position: absolute;
    top: 0;
    left: 0;
    right: 0;
    bottom: 0;
    background: linear-gradient(
      to bottom,
      transparent 50%,
      rgba(0, 0, 0, 0.1) 50%
    );
    background-size: 100% 4px;
    pointer-events: none;
  }
</style>
