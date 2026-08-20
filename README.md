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

Install Omnisave as a background service to keep syncing after you close the
terminal:

```sh
omnisave service install
omnisave service status
omnisave service uninstall
```

Connect to your server and choose games before installing the service. It runs
as your user, starts at boot on supported Linux systems, and starts when you
sign in on macOS and Windows.

On Steam Deck, complete setup in Desktop Mode and accept the background-syncing
prompt before returning to Gaming Mode. Return to Desktop Mode to track newly
installed games.

## Back up and update

Your complete save history is in the Compose `data` volume, under `/data/store` inside the container. Back up that directory while the service is stopped when possible. The store is self-contained, and saves can be recovered without a running server; see [manual recovery](docs/RECOVERY.md).

Update the server in place with:

```sh
docker compose pull
docker compose up -d
```

Update a client in place with:

```sh
omnisave update           # install the newest release
omnisave update --check   # report whether one is available
```

The client verifies the download against the release checksums before replacing
anything and only moves forward unless you pass `--version`. Re-running the
installer updates the client in place too.

## License

Omnisave is released under the [ISC License](LICENSE). Distributed builds also
include the licenses for incorporated material in
[THIRD_PARTY_NOTICES](THIRD_PARTY_NOTICES).
