# Built by the scorer on linux/amd64. Match sandbox: no network, read-only root (writable /tmp),
# 1 CPU, 512 MB, uid 65534, map mounted at /arena. Base image pinned because finalists are rebuilt.
FROM python:3.13.9-slim
WORKDIR /bot
ENV MR_FLAGS=""
COPY mechfight.py bot.py LICENSE ./
ENTRYPOINT ["python3", "-u", "/bot/bot.py"]
