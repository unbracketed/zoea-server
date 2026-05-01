Here's the setup:

Edit `/home/exedev/zoea-server/.env` and replace the placeholder with your real key:

`OPENAI_API_KEY=sk-proj-...`

Then restart: `sudo systemctl restart zoea-server`

How it works: The subprocess (`pi --mode rpc`) is spawned via `exec.Command` which inherits the parent's environment. The systemd unit now loads an EnvironmentFile (.env), so any vars you put there flow through to pi. The file is chmod 600 and gitignored.

You can add other provider keys the same way (e.g. `ANTHROPIC_API_KEY`) — just add lines to .env and restart.