# Using docker compose
```
docker compose up
```

# Using systemd
1. `cp infra/sfw-sasuke.service /etc/systemd/system/`
2. `systemctl enable sfw-sasuke`
3. `systemctl start sfw-sasuke`
