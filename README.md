# Omnisave

Omnisave keeps versioned copies of your game saves in sync across your devices. You host one server, then run the client on every device where you play.

## Set up the server

The easiest way to self-host Omnisave is with Docker Compose:

```sh
cp .env.example .env
docker compose up -d
```

Open `http://your-server:8080` from the same network. The first browser claims the server and chooses a four-digit PIN; other browsers use that PIN to sign in.

On first start, the server also prints an owner token once. Keep it somewhere safe: it provides remote access and recovery if you forget the PIN. To choose the token yourself, set `OMNISAVE_TOKEN` in `.env` before starting the server.

## Install the client

Install the client on every device where you play games.

macOS, Linux, and SteamOS:

```sh
curl -fsSL https://raw.githubusercontent.com/krispya/omnisave/main/scripts/install.sh | sh
```

Windows PowerShell:

```powershell
irm https://raw.githubusercontent.com/krispya/omnisave/main/scripts/install.ps1 | iex
```

The installer verifies the download against the release's checksums and refuses
to install if they disagree. It installs to `~/.local/bin`
(`%LOCALAPPDATA%\omnisave` on Windows) and adds that directory to your shell
startup file if it is not already on `PATH`, naming the file it changed.

On Steam Deck, run the Linux installer from Konsole in Desktop Mode. You can also download a prebuilt archive from the [releases page](https://github.com/krispya/omnisave/releases).

## Connect and sync

Connect each device to your server:

```sh
omnisave connect --server http://your-server:8080
```

Approve the code shown by the client under **Server** in the dashboard. Then start Omnisave:

```sh
omnisave
```

Choose the games to track when prompted. Omnisave performs an initial sync and keeps watching their saves until you quit. To change the selection later, run:

```sh
omnisave track
```

## Keep syncing in the background

Running `omnisave` keeps watching until you close it. On a device you do not
sit in front of — a Steam Deck in Gaming Mode, a machine under the TV — install
it as a background service instead:

```sh
omnisave service install    # run in the background from now on
omnisave service status     # is it actually running?
omnisave service uninstall  # stop and remove it
```

The service runs as you, from the binary you installed, and starts again on its
own after a reboot or a crash. It grants the client nothing a terminal would
not have; it just means nobody has to be there to start it. Connect the device
first — the service has no way to ask you for a server. Linux only for now;
macOS and Windows say so when asked.

On a Steam Deck the whole setup is one Desktop Mode session: run `omnisave`,
approve the pairing code in the dashboard, pick your games, and say yes when it
asks whether to keep syncing. Then go back to Gaming Mode. Games you install
later still need a Desktop Mode visit to start tracking them.

## Back up and update

Your complete save history is in the Compose `data` volume, under `/data/store` inside the container. Back up that directory while the service is stopped when possible. The store is self-contained, and saves can be recovered without a running server; see [manual recovery](docs/RECOVERY.md).

Update the server in place with:

```sh
docker compose pull
docker compose up -d
```

Update a client in place with:

```sh
omnisave upgrade          # install the newest release
omnisave upgrade --check  # report whether one is available
```

The client verifies the download against the release checksums before replacing
anything and only moves forward unless you pass `--version`. Re-running the
installer upgrades the client in place too.

## License

Omnisave is released under the [ISC License](LICENSE).
