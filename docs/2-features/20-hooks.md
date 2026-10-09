# Hooks

The server allows to specify commands that are executed when a certain event happens, allowing the propagation of events to external software.

## runOnConnect

`runOnConnect` allows to run a command when a client connects to the server:

```yml
# Command to run when a client connects to the server.
# This is terminated with SIGINT when a client disconnects from the server.
# The following environment variables are available:
# * MTX_CONN_TYPE: connection type
# * MTX_CONN_ID: connection ID
# * RTSP_PORT: RTSP server port
runOnConnect: curl http://my-custom-server/webhook?conn_type=$MTX_CONN_TYPE&conn_id=$MTX_CONN_ID
# Restart the command if it exits.
runOnConnectRestart: no
```

## runOnDisconnect

`runOnDisconnect` allows to run a command when a client disconnects from the server:

```yml
# Command to run when a client disconnects from the server.
# Environment variables are the same as runOnConnect.
runOnDisconnect: curl http://my-custom-server/webhook?conn_type=$MTX_CONN_TYPE&conn_id=$MTX_CONN_ID
```

## runOnInit

`runOnInit` allows to run a command when a path is initialized. This can be used to publish a stream when the server is launched:

```yml
paths:
  mypath:
    # Command to run when this path is initialized.
    # This can be used to publish a stream when the server is launched.
    # This is terminated with SIGINT when the program closes.
    # The following environment variables are available:
    # * MTX_PATH: path name
    # * RTSP_PORT: RTSP server port
    # * G1, G2, ...: regular expression groups, if path name is
    #   a regular expression.
    runOnInit: ffmpeg -i my_file.mp4 -c copy -f rtsp rtsp://localhost:8554/mypath
    # Restart the command if it exits.
    runOnInitRestart: no
```

## runOnDemand

`runOnDemand` allows to run a command when a path is requested by a reader. This can be used to publish a stream on demand:

```yml
pathDefaults:
  # Command to run when this path is requested by a reader
  # and no one is publishing to this path yet.
  # This can be used to publish a stream on demand.
  # This is terminated with SIGINT when there are no readers anymore.
  # The following environment variables are available:
  # * MTX_PATH: path name
  # * MTX_QUERY: query parameters (passed by first reader) (url-encoded)
  # * RTSP_PORT: RTSP server port
  # * G1, G2, ...: regular expression groups, if path name is
  #   a regular expression.
  runOnDemand: ffmpeg -i my_file.mp4 -c copy -f rtsp rtsp://localhost:8554/mypath
  # Restart the command if it exits.
  runOnDemandRestart: no
```

## runOnUnDemand

`runOnUnDemand` allows to run a command when there are no readers anymore:

```yml
pathDefaults:
  # Command to run when there are no readers anymore.
  # Environment variables are the same as runOnDemand.
  runOnUnDemand:
```

## runOnDemandHTTPAddress

`runOnDemandHTTPAddress` is an alternative to `runOnDemand` for publishers that are started through an API rather than by a local command. When the path is requested by a reader, MediaMTX performs an HTTP request; when there are no readers anymore, it performs a second one. Readers are put on hold until something starts publishing, exactly as with `runOnDemand`.

```yml
pathDefaults:
  # URL to call with an HTTP request when the path is requested by a reader.
  runOnDemandHTTPAddress: https://my-api/streams/initiate
  # Headers of the request, in "Name: value" form.
  runOnDemandHTTPHeaders:
    - 'Content-Type: application/json'
    - 'x-api-key: $MY_API_KEY'
  # Body of the request.
  runOnDemandHTTPBody: '{"stream":"$MTX_PATH","type":"start"}'
  # Body of the request performed when there are no readers anymore.
  runOnUnDemandHTTPBody: '{"stream":"$MTX_PATH","type":"stop"}'
```

The address, headers and body support the same `$VAR` substitutions as `runOnDemand`. Variables that are not among them are looked up in the process environment, which is the recommended way to keep credentials out of the configuration file — in the example above, `$MY_API_KEY` is read from MediaMTX's own environment. Header values are redacted in the responses of the Control API.

Requests are performed asynchronously and never block the path. Transport errors and the status codes 408, 429 and 5xx are retried with an exponential backoff, up to `runOnDemandHTTPRetries` times; demand requests are additionally bounded by `runOnDemandStartTimeout`, since retrying past the moment readers give up serves no purpose. A demand request that is definitively refused (for instance with a 401) is not followed by an un-demand request, since the remote side is known not to have started.

Requests that share a `runOnDemandHTTPKey` are never performed concurrently nor out of order, so an un-demand request can never overtake the demand request it undoes. The key defaults to `$MTX_PATH`; set it to the identifier of the remote stream when the same stream can be reached through multiple path names:

```yml
paths:
  # path names like "mycompany-mylocation/mycamera"
  '~^([^/]+)-([^/]+)/(.+)$':
    runOnDemandHTTPAddress: https://my-api/streams/initiate
    runOnDemandHTTPBody: '{"camera":"$G3","type":"start"}'
    runOnUnDemandHTTPBody: '{"camera":"$G3","type":"stop"}'
    runOnDemandHTTPKey: $G3
```

Note that an un-demand request is also sent when the publisher disconnects on its own and the readers then drain, so the remote endpoint should treat a stop for an already-stopped stream as a no-op.

### Excluding health checks

Some readers should observe a stream without causing it to exist — a health check that periodically fetches a frame, for instance. `runOnDemandExcludeQuery` is a regular expression matched against the query of each reader request; matching readers are **passive**:

- they never trigger a demand request, and are refused immediately when the stream is not available, rather than being held until `runOnDemandStartTimeout`;
- they never keep a stream alive: they do not cancel a pending un-demand, and they are not counted when deciding whether anyone is still watching.

```yml
pathDefaults:
  runOnDemandExcludeQuery: 'type=healthcheck'
```

A probe then reads `rtsp://localhost:8554/mypath?type=healthcheck`: it receives frames while the stream is live, fails fast while it is not, and in neither case changes when the stream starts or stops. Without this, a probe that runs more often than `runOnDemandCloseAfter` would keep resetting the timer and the stream would never stop.

## runOnAvailable

`runOnAvailable` allows to run a command when a stream is available to be read:

```yml
pathDefaults:
  # Command to run when the stream is available to be read.
  # This is terminated with SIGINT when the stream is not available anymore.
  # The following environment variables are available:
  # * MTX_PATH: path name
  # * MTX_QUERY: query parameters (passed by publisher) (url-encoded)
  # * MTX_SOURCE_TYPE: source type
  # * MTX_SOURCE_ID: source ID
  # * RTSP_PORT: RTSP server port
  # * G1, G2, ...: regular expression groups, if path name is
  #   a regular expression.
  runOnAvailable: curl http://my-custom-server/webhook?path=$MTX_PATH&source_type=$MTX_SOURCE_TYPE&source_id=$MTX_SOURCE_ID
  # Restart the command if it exits.
  runOnAvailableRestart: no
```

## runOnUnavailable

`runOnUnavailable` allows to run a command when a stream is not available anymore:

```yml
pathDefaults:
  # Command to run when the stream is not available anymore.
  # Environment variables are the same as runOnAvailable.
  runOnUnavailable: curl http://my-custom-server/webhook?path=$MTX_PATH&source_type=$MTX_SOURCE_TYPE&source_id=$MTX_SOURCE_ID
```

## runOnOnline

`runOnOnline` allows to run a command when a stream is online, which means that the stream is available and provided by a online source (not an offline segment):

```yml
pathDefaults:
  # Command to run when the stream is online, which means
  # that the stream is available and provided by a online source (not an offline segment).
  # This is terminated with SIGINT when the stream is not online anymore.
  # The following environment variables are available:
  # * MTX_PATH: path name
  # * MTX_QUERY: query parameters (passed by publisher) (url-encoded)
  # * MTX_SOURCE_TYPE: source type
  # * MTX_SOURCE_ID: source ID
  # * RTSP_PORT: RTSP server port
  # * G1, G2, ...: regular expression groups, if path name is
  #   a regular expression.
  runOnOnline: curl http://my-custom-server/webhook?path=$MTX_PATH&source_type=$MTX_SOURCE_TYPE&source_id=$MTX_SOURCE_ID
  # Restart the command if it exits.
  runOnOnlineRestart: no
```

## runOnOffline

`runOnOffline` allows to run a command when a stream is not online anymore:

```yml
pathDefaults:
  # Command to run when the stream is not online anymore.
  # Environment variables are the same as runOnOnline.
  runOnOffline: curl http://my-custom-server/webhook?path=$MTX_PATH&source_type=$MTX_SOURCE_TYPE&source_id=$MTX_SOURCE_ID
```

## runOnRead

`runOnRead` allows to run a command when a client starts reading:

```yml
pathDefaults:
  # Command to run when a client starts reading.
  # This is terminated with SIGINT when a client stops reading.
  # The following environment variables are available:
  # * MTX_PATH: path name
  # * MTX_QUERY: query parameters (passed by reader) (url-encoded)
  # * MTX_READER_TYPE: reader type
  # * MTX_READER_ID: reader ID
  # * RTSP_PORT: RTSP server port
  # * G1, G2, ...: regular expression groups, if path name is
  #   a regular expression.
  runOnRead: curl http://my-custom-server/webhook?path=$MTX_PATH&reader_type=$MTX_READER_TYPE&reader_id=$MTX_READER_ID
  # Restart the command if it exits.
  runOnReadRestart: no
```

## runOnUnread

`runOnUnread` allows to run a command when a client stops reading:

```yml
pathDefaults:
  # Command to run when a client stops reading.
  # Environment variables are the same as runOnRead.
  runOnUnread: curl http://my-custom-server/webhook?path=$MTX_PATH&reader_type=$MTX_READER_TYPE&reader_id=$MTX_READER_ID
```

## runOnRecordSegmentCreate

`runOnRecordSegmentCreate` allows to run a command when a recording segment is created:

```yml
pathDefaults:
  # Command to run when a recording segment is created.
  # The following environment variables are available:
  # * MTX_PATH: path name
  # * MTX_SEGMENT_PATH: segment file path
  # * RTSP_PORT: RTSP server port
  # * G1, G2, ...: regular expression groups, if path name is
  #   a regular expression.
  runOnRecordSegmentCreate: curl http://my-custom-server/webhook?path=$MTX_PATH&segment_path=$MTX_SEGMENT_PATH
```

## runOnRecordSegmentComplete

`runOnRecordSegmentComplete` allows to run a command when a recording segment is complete:

```yml
pathDefaults:
  # Command to run when a recording segment is complete.
  # The following environment variables are available:
  # * MTX_PATH: path name
  # * MTX_SEGMENT_PATH: segment file path
  # * MTX_SEGMENT_DURATION: segment duration
  # * RTSP_PORT: RTSP server port
  # * G1, G2, ...: regular expression groups, if path name is
  #   a regular expression.
  runOnRecordSegmentComplete: curl http://my-custom-server/webhook?path=$MTX_PATH&segment_path=$MTX_SEGMENT_PATH
```
