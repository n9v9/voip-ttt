# Tic-Tac-Toe over VoIP

This project lets you play Tic-Tac-Toe by:

1. Visiting a website which displays a phone number and a verification code.
2. Calling the phone number and entering the verification code.
3. Waiting for another player to do the same.
4. Playing Tic-Tac-Toe by selecting fields through pressing numbers on your
   phone and seeing the live updates on the website.
5. Speaking with your opponent by using your phone's microphone and your
   browser's audio context.

See [ARCHITECTURE](./ARCHITECTURE.md) for implementation details.

## Build

Build the server:

```sh
cd ./cmd/vt-server
go build
```

Build the client:

```sh
cd ./cmd/vt-client
go build
```

## TODO

-   Better build instructions
-   Replace `chan_sip.so` with `chan_pjsip.so`
-   Use the heartbeat webhook to detect when the client hung up the phone
-   Rewrite `ARCHITECTURE.md`
-   Containerize the whole application, or at least asterisk?
