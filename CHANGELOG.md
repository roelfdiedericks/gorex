# Changelog

The version of each release is its heading here, which `mygo.json`'s
`version` must match: `make website` refuses to build otherwise, and
refuses a version already published.

## 0.1.12

- Import Hosts from PuTTY / KiTTY… (Windows, in the palette) makes hosts
  of the saved ssh sessions of PuTTY and KiTTY: in the registry, or in
  the folder of a KiTTY that keeps them in one, found by its kitty.ini or
  chosen. Each becomes a host named as its session, with its user (a
  session without one takes Default Settings'), port, agent forwarding,
  ssh jump host, compression and key. The dialog says what doesn't carry
  over, as other proxies or a remote command, leaves out the hosts GoRex
  has, and has Select All / Select None. ssh can't read PuTTY's .ppk
  keys: the host's settings say to export one from PuTTYgen.
- The host popover filters its hosts as you type, by name or address:
  Enter switches to the one left, or the one chosen with ↑↓, and
  otherwise connects to what you typed, as before. An address picks only
  a host of that address in full. A line under the field says what Enter
  does; Escape clears the text first.
- Each host in the list has its machine's icon, as the host chip's, and
  the window's own has a dot of its connection's color. A host shows its
  machine's icon once connected to with this version.
- omp's composer, with GoRex's TSP: an image pasted in a message stays
  in it when sent, as omp's own Enter does, also while omp's turn runs.

## 0.1.11

- Serial ports: Add Host…, Connect with Serial port, makes a host of a
  port of this PC, as a switch's, a router's or a board's console. The
  dialog lists the ports as they are plugged in, each with its adapter's
  name over its path, USB IDs and serial number, marking one another
  host has open or one not plugged in; it takes the speed (115200 by
  default, or any other), the framing (8N1, 7E1… or data bits, parity
  and stop bits), what Enter and Backspace send, and local echo. The
  host is named by its port first, as "COM8 · USB-Enhanced-SERIAL
  CH9102 · 115200", and the host chip tells the port too.
- A serial host's window has the port's one session, in a session server
  of its own on this PC, which keeps the port open while GoRex is
  closed. An adapter unplugged is waited for and found again, by its
  serial number or USB IDs, wherever it comes back. Send Break (⇧⌘B,
  Ctrl+Shift+B) sends a break; Release Port closes the port for another
  program, and Enter opens it again. Settings changed in the dialog apply
  to the open port at once. A port another serial host has open, the
  pane says whose, and Enter takes it over. `gorex serial <name>` opens a
  window on one.
- The host chip has a menu when right-clicked: Reconnect, Host
  Settings…, Open in New Window, Copy Address and Copy ssh Command, the
  Connection Log, Restart Session Server… (which says first whether the
  sessions keep running: they do on Linux and macOS hosts), Switch to the
  hosts used last, Add Host… and Settings…. The palette has Host
  Settings… and Restart Session Server… too.
- The host popover is wider and scrolls when the window is too short for
  it. Its ssh user@host field comes before the hosts and takes the
  keyboard as it opens. Each host's actions show always, in columns:
  settings (This PC's open the Defaults), a window of its own or
  reconnect, and forget, which asks first.

## 0.1.10

- Plain ssh hosts: a host where GoRex's server can't run, or that you
  want so, is reached with ssh alone. Its tabs and panes each run `ssh`
  from this PC, in a session server of the host's own here, with its own
  tabs and layout, and it is switched to from the host list as any host.
  Its sessions outlive the window, but not their connections: one that
  ends other than by logging out says so, and Enter connects it again.
  Editor tabs and the files of omp's cards, which need GoRex's server on
  the host, show greyed out.
- Host settings: Add Host… (in the host list, Settings, Hosts, and the
  palette) and each host's settings (⚙ in the host list, Edit… in
  Settings, Hosts) give a host a name, its ssh destination, an identity
  file, more ssh options every ssh to it takes (`-p 2222 -o
  ProxyJump=bastion`), Forward ssh agent, and how GoRex reaches it: with
  its server there, in a folder of your choice (`~/.cache/gorex` when
  empty), or with plain ssh, with a command to run there. Serial ports
  are to come.
- When GoRex's server can't run on a host (no server for its system, too
  old a glibc, or a folder that runs no programs, as a home mounted
  noexec), Settings, General, *When GoRex's server can't run on a host*
  decides: ask (the default), with Try other folders and Connect with
  plain ssh; try other folders, then plain ssh; or plain ssh. Other
  folders are folders of your own in the runtime folder, `/var/tmp`,
  `/dev/shm` and `/tmp`, the first where a program runs. The host's
  settings keep what was done, and later connections follow them.
  Failing logins and hosts out of reach never fall back.
- A folder GoRex makes for its server on a host is yours alone, and
  GoRex runs no server from a folder of another user's.
- `gorex ssh --plain user@host` connects with plain ssh, and `--server`
  with GoRex's server, over the host's settings.
- ssh's password prompts and the question of a host where GoRex's server
  can't run show over the settings and every other dialog.

## 0.1.9

- GoRex connects to Linux hosts with glibc 2.17 or later, as Debian 8,
  Ubuntu 14.04 and RHEL 7 have: the terminal library its server loads
  there is GoRex's own, built for glibc 2.17, where MyGo's needs 2.28.
  On a Debian 9 host (glibc 2.24) no session started: loading the
  library failed with "version `GLIBC_2.27' not found".
- A host whose glibc is older than the server needs gets nothing copied
  and nothing started: GoRex says so, with the host's version and the
  one needed, instead of a session failing to start.

## 0.1.8

- A long omp transcript draws fast: of the hundreds of messages and
  tools of a day's session, only those near the view are built and laid
  out each frame, the rest standing as space of the height they were
  measured at, until they change or come near. On a transcript of 815
  of them, a frame took 37 ms while scrolling (27 frames a second) and
  38 ms while a command ran; it takes 6 ms and 4 ms. Switching back to
  its tab took half a second; it takes 4 ms.
- Resizing a window over a long omp transcript is smooth: while the
  width changes, only what is near the view is laid out again, the
  transcript drawn from its end; the rest is measured again a few
  messages a frame once the width stands. On the same transcript a
  frame of the drag took 157 ms on average and up to half a second; it
  takes 8 ms, and the second after it 13 ms a frame.
- After a narrower window was made wider again, the end of omp's
  transcript stood above a blank band over the composer, or out of the
  view: the terminal keeps the rows it no longer needs in its
  scrollback. The transcript now ends right over the composer.
- Leaving omp from its composer, as with Ctrl+C twice, gives the keys
  back to the terminal: the shell after it took none until a click.
- Desktop notifications are off unless Settings turns them on (Show
  desktop notifications): those of long commands, of programs' status
  and those programs send. On Windows each came with an icon in the
  notification area, one for each GoRex running, which could stay.
- omp's BTW history reads well: the answer takes the room its list of
  questions leaves, rather than a strip as narrow as a word, as GoRex
  now follows the sizes programs give their nodes (shrink, min and max
  widths and heights, a row's align). Page Up, Page Down, Home and End
  in the answer scroll the sheet, not the terminal under it. A list's
  selected item no longer pins the scroll of what holds it.
- An overlay of panes side by side, as the BTW history's questions
  beside the answer, is a sheet of its own height: each pane scrolls on
  its own, the title and the buttons stay in view.

## 0.1.7

- omp's transcript scrolls with the keys, as in Tern: Page Up and Page
  Down by a page, Ctrl+Home and Ctrl+End to its start and end, with the
  focus in the composer or not, and Home and End too outside the
  composer, where they move its caret. They reached omp, which does not
  scroll its transcript itself. The composer takes the focus a click on
  the dock or on omp's controls, or a pane switch, gives the terminal; a
  click in the transcript leaves it. While omp shows a picker or a
  sheet, its keys stay its own.
- omp's composer stays at the bottom of the pane when the scrollback is
  scrolled above omp's start, as Tern's does: it went away with the
  focus in it, and no key scrolled back down.

## 0.1.6

- omp's dock floats over its transcript as Tern's does: centered and at
  most 800 DIPs wide, its composer a card with the context meter a
  hairline along its top edge (the share used over the fill's end, the
  window's size at the right; a glow sweeps it while a turn runs), the
  working row above the card (a star, the time, the label on one line,
  and the todo list as a pill at the right that shows the whole plan when
  clicked), and in the card the session's title as the input's
  placeholder, the model as a pill, the rate, the pane's folder, the git
  branch on one line, the cost, and a round send or stop button. Empty
  parts of a program's surface take no room: a band of blank space sat
  over the context meter.
- Text a program prints under its open surface, where it is to print
  nothing, is erased: the ends of its lines showed beside the surface, in
  the strip kept clear for the scroll bar.
- Settings have an OMP Settings page, for programs that speak the Tern
  Surface Protocol, such as omp: the protocol's switch (moved from the
  Terminal page, and now a Default for every host as well as a host's
  own), the font, size and line height of their text, the font, size and
  ligatures of their code (commands and output, diffs, code blocks,
  paths), which follow the terminal's until set, the composer's and the
  messages' widths at most, the spacing, and whether the working
  indicators move. A host's switch set on the Terminal page carries over.
- omp's slash commands and other lists at its composer's caret open
  above the composer, as wide as it: they were squeezed under the input,
  inside the composer. A list anchored elsewhere goes to the side with
  room when its own has too little.
- In omp's composer, Ctrl+C goes to omp unless text is selected there
  (and Ctrl+C copies a selection, as the terminal's setting says), so
  pressing it twice exits; Alt with a letter or digit goes to omp too, so
  Alt+P switches the model. On macOS, Option does so with Option as Alt.

## 0.1.5

- `gorex ssh -A user@host` forwards the ssh agent to the host for that
  window, and `-a` doesn't, as ssh's own -A and -a, over the host's
  Forward ssh agent switch and `~/.ssh/config`.

## 0.1.4

- ssh agent forwarding works with sessions that stay: where
  `~/.ssh/config` says `ForwardAgent yes`, or a host's new Forward ssh
  agent switch (Settings, Hosts) is on, the host's sessions use the
  agent of the window connected now, the latest one's, at a path that
  stays, after a reconnect too. They had the first connection's agent,
  which was gone with it.
- When a host's GoRex server does not start after it was copied there,
  the Connection Log says why: no server for the host's system, none
  there, or one there that can't be run, with its files' modes and the
  mount it is on (noexec); and where the copy went, or that one was
  there already. Connecting then stops, with Retry, rather than trying
  again by itself and asking for a passphrase each time; and a server
  folder there whose server can't be run is replaced by the copy, where
  it was kept.

## 0.1.3

- The file browser marks the files git sees as changed in a repository
  reached through a link, as macOS's /tmp, /var and /etc are, or a
  project folder of the user's that links elsewhere: it marked none.

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
