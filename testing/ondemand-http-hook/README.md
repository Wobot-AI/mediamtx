# Testing the HTTP on-demand hook

A self-contained compose setup for exercising `runOnDemandHTTPAddress` /
`runOnDemandExcludeQuery` independently. MediaMTX is built from the repo's
working tree (not a release tarball), so it always reflects what's on disk.

## 1. Against the real staging API

```sh
cp .env.example .env   # fill in the real WOCAM_API_KEY
docker compose up --build
```

In another terminal, demand a path (replace `cmp1-loc1/cam1` with a real
`cmpid-locid/camid`):

```sh
ffprobe -rtsp_transport tcp rtsp://localhost:8554/cmp1-loc1/cam1
```

Watch `docker compose logs -f mediamtx` for `runOnDemandHTTP request launched`
/ `runOnUnDemandHTTP request launched`, and confirm on the staging side that
the camera actually starts and stops.

## 2. Fully offline, with a fake smart-streamer

No real API or camera needed — a mock server publishes a test pattern into
MediaMTX on "start" and kills it on "stop".

Edit `mediamtx.yml` and swap the address:

```yaml
runOnDemandHTTPAddress: http://mock-api:9999/api/v1/camera/smart-streamer/initiate
```

Then:

```sh
echo "WOCAM_API_KEY=anything" > .env
docker compose --profile mock up --build
```

Demand the path (must match `acme-nyc/<camera>`, since that's what the mock
publishes into — see `mock-api/server.py`):

```sh
ffprobe -rtsp_transport tcp rtsp://localhost:8554/acme-nyc/cam42
```

You should see in `docker compose logs -f`:
- `mock-api`: `start camera=cam42 key='...'`, then it starts publishing
- `mediamtx`: `stream is available and online`, `is publishing to path ...`
- after you disconnect and `runOnDemandCloseAfter` (10s) elapses: `mock-api`
  logs `stop camera=cam42`, and `mediamtx` logs `runOnUnDemandHTTP request
  launched`

## 3. Health-check / passive reads

A request whose query matches `runOnDemandExcludeQuery` (`type=healthcheck`
by default here) never starts or sustains the stream:

```sh
# while nothing is publishing: fails immediately, no API call is made
ffprobe -rtsp_transport tcp "rtsp://localhost:8554/acme-nyc/cam42?type=healthcheck"

# while a real viewer is already watching: succeeds and returns frames,
# but repeating this does not keep the stream alive once the real viewer leaves
ffprobe -rtsp_transport tcp "rtsp://localhost:8554/acme-nyc/cam42?type=healthcheck"
```

## Cleanup

```sh
docker compose --profile mock down
```
