FROM python:3.12-alpine
RUN apk add --no-cache ffmpeg
COPY server.py /server.py
ENTRYPOINT ["python3", "/server.py"]
