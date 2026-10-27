# Changelog

The version of each release is its heading here, which `mygo.json`'s
`version` must match: `make website` refuses to build otherwise, and
refuses a version already published.

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
