# Local deployment methods

## Running outside of container
1. `cd sfw-sasuke`
2. `go build`
3. Start service andd use env variables from file: `./sfw-sasuke -useEnvFile`

## Using docker
1. `cd sfw-sasuke`
2. `docker run -d --mount type=bind,src="$(pwd)/env",dst=/app/env --mount type=bind,src="$(pwd)/static",dst=/app/static sfw-sasuke`

## Using docker compose
1. `cd sfw-sasuke`
2. `docker compose up`

## Using systemd
1. `cd sfw-sasuke`
2. `cp infra/sfw-sasuke.service /etc/systemd/system/`
3. `systemctl enable sfw-sasuke`
4. `systemctl start sfw-sasuke`

# Secrets format
For now, a `secrets.env` file should be located in the `env` directory. The contents should have the format `BOT_TOKEN=YOUR_DISCORD_BOT_TOKEN`.
