# On-demand publishing

Edit `mediamtx.yml` and replace everything inside section `paths` with the following content:

```yml
paths:
  ondemand:
    runOnDemand: ffmpeg -re -stream_loop -1 -i file.mp4 -c copy -f rtsp rtsp://localhost:$RTSP_PORT/$MTX_PATH
    runOnDemandRestart: yes
```

The command inserted into `runOnDemand` will start only when a client requests the path `ondemand`, therefore the file will start streaming only when requested.

When the publisher is not a local command but a remote service that is started through an API, use `runOnDemandHTTPAddress` instead:

```yml
paths:
  ondemand:
    runOnDemandHTTPAddress: https://my-api/streams/initiate
    runOnDemandHTTPHeaders:
      - 'Content-Type: application/json'
      - 'x-api-key: $MY_API_KEY'
    runOnDemandHTTPBody: '{"stream":"$MTX_PATH","type":"start"}'
    runOnUnDemandHTTPBody: '{"stream":"$MTX_PATH","type":"stop"}'
    # allow the remote service enough time to start publishing
    runOnDemandStartTimeout: 30s
```

MediaMTX will call the API when the path is requested, hold readers until the remote service starts publishing, and call it again once the last reader has left. See [runOnDemandHTTPAddress](./20-hooks.md#runondemandhttpaddress) for the full set of options.
