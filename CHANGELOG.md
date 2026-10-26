# Changelog

The version of each release is its heading here, which `mygo.json`'s
`version` must match: `make website` refuses to build otherwise, and
refuses a version already published.

## 0.1.2

- A pane scrolls as one document with one scroll bar, the terminal's:
  a program's surface, such as omp's transcript, takes rows among the
  output, and a long one is whole and its end in reach. Scrollback is
  kept in lines at any width, as its setting says (it held a fraction
  of them in wide panes), up to 50,000; a transcript longer than the
  scrollback loses its start, as output does, never its end.
- The terminal's scroll bar is the one everything else in the window
  has: 10 DIPs wide, the same under the pointer and dragged, shown while
  the pointer is over the pane or the view is scrolled up; a click on
  its track scrolls a page. Surfaces and omp's composer leave it clear
  at the pane's edge.
- Scroll bar width, Scroll handle length (at least; 48 DIPs built in, so
  a long scrollback's handle is no sliver), and Always show scroll bars,
  in Settings, Defaults: Appearance, for the terminal and everything
  else that scrolls.
- `gorex doctor [user@host]`: which ssh GoRex runs among those on the
  PATH, whether ssh's agent holds keys, and, for a host, a connection
  made as GoRex makes it, with why it fails in words (a host that wants
  a password, keys it refuses). `gorex help` lists the commands. On
  Windows they come from gorex.com, a console program beside GoRex.exe
  in the installed app's bin, which a shell runs for `gorex`: the shell
  waits for it and gets its exit status, and it starts GoRex.exe for
  everything else.
- ssh asks in the window what it would ask in a terminal: a password,
  a key's passphrase, a one-time code, whether to trust a host's key it
  has not seen (it failed instead). The answers last one connect: a
  reconnect asks again. A login cancelled, a password ssh stopped asking
  for, a host key not trusted or changed stop the connecting, with
  Retry, rather than trying again by themselves.

## 0.1.1

- Programs that speak the Tern Surface Protocol, such as the coding
  agent omp, draw natively: their messages, tool calls, pickers,
  settings, forms, pictures, math and composer as GoRex's own controls,
  in their colors, among the terminal's output and kept in its
  scrollback. Turn it on for a host in Settings, Programs: Tern Surface
  Protocol (experimental); omp then starts natively by itself.
- A new icon: a rex in cyan on a dark tile.

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
