# Runtime expects the map mounted read-only at /arena.
FROM python:3.13.9-slim
WORKDIR /bot
ENV MR_FLAGS=sneak
COPY mechfight.py bot.py LICENSE ./
ENTRYPOINT ["python3", "-u", "/bot/bot.py"]
