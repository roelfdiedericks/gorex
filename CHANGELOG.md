# Changelog

The version of each release is its heading here, which `mygo.json`'s
`version` must match: `make website` refuses to build otherwise, and
refuses a version already published.

## 0.1.0

The first release from gorex.goclaw.org.

- Tabs and split panes over sessions that outlive the app, kept by a
  session server; windows show each session through frames of the
  server's terminal, so a resize or a reattach never garbles a screen.
- Session servers on other hosts over ssh: GoRex copies its server there
  and reconnects when the link drops.
- An editor with a file tree, git changes, and a Markdown preview.
- Settings for the app and for each host.
- Images of the Kitty graphics protocol, pasting images into programs
  such as coding agents, and their program status (OSC 7501).
- A `gorex` command on the PATH from the Windows installer.
- Updates itself from gorex.goclaw.org: Check for Updates… in the command
  palette.
