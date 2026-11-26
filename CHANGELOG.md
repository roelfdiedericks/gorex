# Changelog

Each release's version is its heading in this file. The `version` in
`mygo.json` must match the top heading. Otherwise `make website` refuses
to build. It also refuses to build a version that is already published.

## 0.1.21

- omp's background work row shares its width between its cards when
  there are only a few, as in Tern, so a job's name and progress are no
  longer cut short.
- A background job's details show omp's summary of the job beside its
  last lines of output, as in Tern, with its progress bars across the
  space they have.
- The details of finished work list each subagent on one line, as in
  Tern: its name and what it reported, with the model, effort, time,
  tools and cost at the right end. The list scrolls, and lights the line
  under the pointer.
- Opening a subagent from omp's background work closes its details, as
  in Tern. While the row shows the subagent you're viewing, the
  composer's Viewing line hides; otherwise it shows the subagent's name
  in a pill, with esc main at the right end to go back.
- Key caps read esc and show a return arrow for Enter, as in Tern.
- A large paste into omp's composer, more than 10 lines or 1,000
  characters, goes to omp as in Tern: omp collapses it into a chip, or
  asks how to attach it when it's very long. Smaller pastes go straight
  into the composer, as before.
- Fixed: pasting text that a program reads itself, as omp does with a
  large paste, showed "Image pasted". It now says "Pasted", and only an
  image says image.

## 0.1.20

- Drag a terminal into an editor tab too, as a file could already go into
  a terminal tab. With the terminal focused, a file opened from the tree
  opens in an editor beside it.
- Drag a pane onto the New Tab button, or onto the empty part of the
  title bar after the tabs, to move it into a tab of its own. A file's
  pane goes into a new editor tab, with the tree open at its folder.
- A terminal that was alone in an editor tab leaves an empty editor there
  when you drag it away, so the tab keeps its tree.
- Fixed: the copy of a pane that follows the pointer while you drag it
  was not drawn.
- omp's background work shows on top of the composer, as in Tern. While
  something runs, a row of cards shows each background job and
  subagent, newest first, with its status, time and latest output; it
  scrolls sideways when there are more, with a bar of all of them under
  it. Click a card to see its details, with Open, Stop and Dismiss;
  double-click it or press Enter to open that subagent's conversation,
  its card marked with an eye, and click MAIN to go back. When nothing
  runs, the status line shows a dot for the newest eight and how many
  more there are: point at a dot to see its card, and click it to show
  the row. Escape closes a card's details, then folds a row shown from
  the dots back to dots. A click in the composer does both at once; a
  row of running work stays. The row and a card's details open and
  close smoothly, and the conversation moves with them.
- While omp works, a streak crosses the composer's context line from end
  to end, as in Tern, instead of a glow inside the part used.
- While you scroll back through an omp conversation, the text under the
  composer's strip is blurred, as in Tern, so the composer reads clearly
  over it.
- The context line over omp's composer shows its two marks as icons, as
  in Tern: where the compaction summary starts, and where auto-compact
  runs. Point at an icon to see what it marks; the whole line lights up
  under the pointer, and a click anywhere on it opens the Context
  dialog. Past the summary's mark, the line shows in the accent color.
- omp's Context dialog looks as in Tern: the tokens used in large type,
  a full-width bar colored by category with the auto-compact buffer
  hatched, and a narrower window.
- In light themes, omp's composer is a touch lighter than the
  conversation behind it, as in Tern.

## 0.1.19

- A terminal pane's header has a Show Files button beside Split Right,
  which opens or closes the tab's file panel, as Ctrl+Shift+E (⇧⌘E on
  macOS) does.
- Drag a pane by its header to move it: onto a side of another pane to
  split it, onto its middle to swap them, onto the edge of the tab to take
  a whole side, or onto a side of its own place to split it. The panes
  move aside as you drag, to show where it will land. Hold it over another
  tab to open that tab and drop it there.

## 0.1.18

- Packet captures (`.pcap`, `.pcapng`, `.cap`, and gzipped ones) open in
  webshark, Wireshark in the window, from the file panel, an editor tab's
  tree or a link, on ssh hosts too. A capture that is still being written,
  as by `tcpdump -w`, grows in the packet list. RTP audio may not play
  yet.

## 0.1.17

- Terminal tabs can show a file panel, as in Tern: press Ctrl+Shift+E
  (⇧⌘E on macOS) or choose Show Files in the command palette. The panel
  follows the shell: after a `cd`, it moves to the new folder once the
  shell is back at its prompt. The pin in its header keeps it on a folder.
  Click a file to open it in the popup over the terminal. Right-click for
  New Tab Here, Split Here, cd Here, Copy Path and more.
- Split Down's shortcut is now Ctrl+Shift+− on Windows and Linux, because
  Ctrl+Shift+E opens the file panel. On macOS it stays ⇧⌘D.
- Peek: rest the pointer on a file in the file panel or an editor tab's
  tree, and a card shows the start of the file: Markdown rendered, code
  highlighted, pictures. Space shows it for the chosen file. Turn it off
  in Settings › Editor.
- The file panel and the editor tab's tree can create, rename, duplicate,
  cut, copy, paste and delete files and folders, on ssh hosts too. Move to
  Trash uses the Recycle Bin on Windows, the Trash on macOS, and the
  desktop's trash on Linux where there is one. GoRex always asks before
  moving to the trash or deleting.
- Reload Window (Ctrl+Shift+F5, ⇧⌘F5 on macOS) starts the window again on
  the same host, with its tabs and sessions.
- Markdown files now open in MyGo's native preview. It draws Mermaid
  diagrams, follows links to headings and to other files, and its tables
  have clearer lines and shaded rows. To use the web view's preview
  instead, change Settings › Editor › Markdown preview. The chip in the
  preview's header switches between the two.
- Click the file name in one of omp's Read or Edit cards, and the file
  opens at the lines omp read or changed, coloured: green for a read,
  amber for an edit. Markdown previews colour the paragraphs, lists and
  code blocks those lines are in. The colour goes away when you edit the
  file.
- Drag files or folders from your desktop onto the file panel or an
  editor tab's tree to copy them to the host, ssh hosts included. Drop on
  a folder to put them in it. Drop on a terminal to upload them to the
  folder its program is in and paste their paths at the prompt. A toast
  shows the progress, with Cancel. When a name already exists, GoRex asks
  whether to replace it, keep both or skip it. Files keep their
  modification times.
- Drag a file or folder from the file panel or an editor tab's tree to
  your desktop or a folder to copy it there, from ssh hosts too. On
  Windows, Explorer copies it with its own progress window while GoRex
  stays responsive. On macOS and Linux, GoRex downloads the copy as the
  drag starts, with a toast showing the progress.
- Fixed: labels in omp's charts were drawn twice, slightly apart.
- Fixed: the mouse wheel could not scroll long lists in omp, such as
  `/login`'s providers, past the chosen row.
- Fixed: in the preview of a Markdown file, a code block without a
  language, or with one GoRex doesn't colour, showed as one run-together
  paragraph. It now shows as code, with its lines and indentation.
- Fixed: the MyGo preview of a Markdown file kept showing the file as it
  was when the preview opened, after the file changed on disk.
- This version's session server knows the new file operations. On
  Windows, the server already running keeps running, with your sessions;
  until you use Restart Session Server…, the file operations say that the
  server needs replacing.

## 0.1.16

- omp's native view (the Tern Surface Protocol) is now on by default, and
  no longer marked experimental. If you had turned it off, it is on again
  once; turn it off in Settings › OMP Settings if you prefer. Tabs that
  are already open keep how they started; new tabs and panes, and Restart
  Shell in Pane, use it.
- On Linux the window has rounded corners, as on macOS and Windows. GoRex
  draws its own minimize, maximize and close buttons there, in the order
  your desktop uses.
- Mermaid diagrams stay sharp at any zoom in the picture viewer. Labels on
  the arrows of flowcharts no longer have the line running through them.
- A file that a message names before omp writes it becomes a link once the
  file exists.
- Built on a newer MyGo: updates retry when a download fails or stalls,
  links in the terminal that wrap onto the next line open whole, and
  drawing is faster in several places. A scroll bar's handle now widens a
  little while you drag it.

## 0.1.15

- Files that omp names open with a click. A file name in a tool card
  (Write, Edit, Read) or in a message, such as `FOO.md`, is a link: a
  click shows the file in a popup over the pane, and Ctrl+click (⌘ on
  macOS) opens it in the editor tab. You can change both in Settings ›
  OMP › Files. A file's menu has Open in Popup, Open in Editor Tab and
  Copy Path.
- The popup shows the file as the editor tab does, Markdown as a
  preview. You can edit and save it there. Its buttons keep the file in
  a split beside the pane or in the editor tab, make the popup fill the
  window, or close it. Escape or a click outside closes it too, and asks
  first about unsaved changes.
- A file kept in a terminal tab stays there after a restart. Its new
  button moves it to the editor tab.
- Save, Show Preview, Show Changes and Soft Wrap now work for any file
  that has the keys, in the popup and in terminal tabs too.
- Open files now follow changes on disk in every tab and in the popup.
  A file without edits reloads; one with edits says it changed on disk.
  Pictures and PDFs show again when they change.
- Mermaid diagrams in omp's messages are drawn as diagrams, with Source,
  Copy and Open. A diagram that has an error shows its code and says
  why it can't be drawn.
- Pictures and diagrams open in a viewer you can zoom and move. The
  mouse wheel or a pinch zooms, dragging or a touchpad moves the
  picture, and a double-click switches between fitted and 100%. The
  keys + - 0 1 and the arrows work too. Diagrams stay sharp when zoomed.
- Cards that fold show a chevron that turns as they open and close.
- omp's status line has its charts and hover cards, as in Tern. The
  background jobs list, the agents list and omp's settings look like
  Tern's.
- When you scroll back in omp's messages, a button takes you back to the
  end.
- omp's sign-in Copy and Open buttons work. Links in omp's text open,
  and their menu copies them.
- omp's settings open in the middle of the window, and their search
  takes your typing.
- New settings: Scroll speed and Smooth scrolling (Settings ›
  Appearance), Compress scrollback (Settings › Terminal), Message size
  (Settings › OMP › Text).
- Transparent terminal background is now Terminal background opacity, a
  slider set to 90% by default, so that color schemes look like
  themselves.
- The terminal theme picker lists light themes for light windows and
  dark themes for dark ones. Show all themes lists every theme.
- The mouse wheel scrolls three lines per notch on Windows, as the
  system does, instead of about seven.
- omp's messages stay still while you read them during a turn, and
  sending a message brings the view to the end.
- Updating GoRex also updates the servers on your ssh hosts, and
  removes old ones. The Connection Log says what happens in plain words,
  its text can be selected, and it has a Copy button.

## 0.1.14

- Copy on select is now on by default on Windows and Linux, as in PuTTY:
  selecting text in a terminal pane copies it. On macOS it stays off. If
  you changed the setting yourself, your choice stays.
- The text in GoRex is easier to read: notes, hints, errors, dialogs,
  `gorex help` and `gorex doctor` now use plain English.
- Buttons and menu items now use the same capitalization as the command
  palette, for example Try Other Folders and Trust and Connect.
- Some names are clearer:
  - Set Root to Home Folder, in the file browser's menu.
  - Copy appearance settings, in Settings › Hosts.
  - Minimum scroll handle length, Maximum composer width and Maximum
    message width, in Settings.
  - The dialog for a host key that ssh has not seen is titled "New host".

## 0.1.13

- New setting on Windows: *ssh agent*, in Settings › Hosts. It chooses
  the agent that GoRex's ssh uses to log in.
- The default, Automatic, uses Pageant (or KiTTY's Kageant) when it is
  running. GoRex then logs in to hosts with the keys loaded in Pageant,
  and Forward ssh agent passes those keys on. When Pageant is not
  running, GoRex uses the Windows ssh-agent.
- You can also choose Pageant or the Windows ssh-agent directly.
- A dot marks the agent in use: green when it answers, red when it does
  not. The line under it shows how many keys the agent holds.
  `gorex doctor` shows this too.
- ssh cannot read PuTTY keys (.ppk files). For a host that uses one,
  Host Settings now shows whether the key is already in the agent.
- If it is not, Host Settings can convert the key to OpenSSH format.
  Enter the key's passphrase if it has one. The converted key goes into
  your .ssh folder with the same passphrase and a .pub file beside it.
  An existing file is never overwritten.
- Convert PuTTY Key… in the palette converts any PuTTY key.
- Conversion supports PuTTYgen formats 2 and 3, with RSA, ECDSA and
  Ed25519 keys.
- The import dialog shows what each session's key needs.

## 0.1.12

- New palette command on Windows: Import Hosts from PuTTY / KiTTY….
  It creates hosts from the saved ssh sessions of PuTTY and KiTTY.
- It reads sessions from the registry. It also reads sessions that
  KiTTY stores as files in a folder. GoRex finds that folder through
  kitty.ini, or you can choose it.
- Each session becomes a host with the session's name. The host gets the
  session's user, port, agent forwarding, ssh jump host, compression and
  key. A session without a user gets the user from PuTTY's Default
  Settings.
- The dialog shows what does not carry over, such as other proxies or a
  remote command. It leaves out hosts that GoRex already has. It has
  Select All and Select None.
- ssh cannot read PuTTY's .ppk keys. For such a host, Host Settings tells
  you to export the key from PuTTYgen.
- The host popover now filters hosts by name or address as you type.
- Enter switches to the only remaining host, or to the host you chose
  with ↑↓. Otherwise Enter connects to what you typed, as before.
- If you type an address, only a host with exactly that address matches.
- A line under the field shows what Enter will do. Escape clears the
  text first.
- Each host in the list shows its machine's icon, like the host chip.
  The current window's host also has a dot in its connection's color.
- A host shows its machine's icon after you connect to it once with this
  version.
- Fixed: with the Tern Surface Protocol on, an image pasted into omp's
  composer was lost when you sent the message. It now stays in the
  message, as with omp's own Enter, also while omp is working on a turn.

## 0.1.11

- Serial ports: in Add Host…, set Connect with to Serial port to create
  a host for a serial port on this PC, such as the console of a switch,
  a router or a board.
- The dialog lists the ports and updates as you plug them in. Each port
  shows its adapter's name above its path, USB IDs and serial number.
  The dialog marks a port that another host has open, and a port that is
  not plugged in.
- The dialog sets the speed (115200 by default, or any other) and the
  framing (8N1, 7E1…, or data bits, parity and stop bits). It also sets
  what Enter and Backspace send, and local echo.
- The host's name starts with the port, for example
  "COM8 · USB-Enhanced-SERIAL CH9102 · 115200". The host chip shows the
  port too.
- A serial host's window has one session for the port. The session runs
  in its own session server on this PC, which keeps the port open while
  GoRex is closed.
- When an adapter is unplugged, GoRex waits for it. It finds the adapter
  again by its serial number or USB IDs, whichever port it comes back on.
- Send Break (⇧⌘B, Ctrl+Shift+B) sends a break signal.
- Release Port closes the port so that another program can use it. Enter
  opens it again.
- Settings you change in the dialog apply to the open port at once.
- If another serial host has the port open, the pane shows which host.
  Enter takes the port over.
- `gorex serial <name>` opens a window for a serial host.
- Right-click the host chip for a menu: Reconnect, Host Settings…, Open
  in New Window, Copy Address, Copy ssh Command, Connection Log, Restart
  Session Server…, the most recently used hosts to switch to, Add Host…
  and Settings….
- Restart Session Server… first tells you whether the sessions keep
  running. They do on Linux and macOS hosts.
- The palette also has Host Settings… and Restart Session Server….
- The host popover is wider. It scrolls when the window is too short
  for it.
- The ssh user@host field now comes before the hosts and has the
  keyboard focus when the popover opens.
- Each host's actions are always visible, in fixed columns: settings,
  open in a new window (reconnect, for the window's own host), and
  forget. For This PC, settings opens the Defaults. Forget asks first.

## 0.1.10

- Plain ssh hosts: GoRex can reach a host with ssh alone. Use this when
  GoRex's server cannot run on the host, or when you prefer it.
- Each tab and pane of a plain ssh host runs `ssh` from this PC. These
  sessions run in a separate session server on this PC for that host,
  with their own tabs and layout. You switch to the host from the host
  list like any other host.
- The sessions outlive the window, but their connections do not. When a
  connection ends for a reason other than logging out, the pane says so,
  and Enter connects again.
- Editor tabs and the files on omp's cards need GoRex's server on the
  host, so they are greyed out on plain ssh hosts.
- Host settings: Add Host… is in the host list, in Settings › Hosts and
  in the palette. To change a host, click ⚙ in the host list, or Edit…
  in Settings › Hosts.
- In Host Settings you set the host's name, its ssh destination, an
  identity file, and extra ssh options for every connection to it (for
  example `-p 2222 -o ProxyJump=bastion`). You can also turn on Forward
  ssh agent.
- You also choose how GoRex reaches the host: with GoRex's server in a
  folder you choose (`~/.cache/gorex` when empty), or with plain ssh and
  a command to run on the host.
- Serial ports will come in a later release.
- Sometimes GoRex's server cannot run on a host: there is no server for
  its system, its glibc is too old, or the folder cannot run programs
  (for example a home folder mounted noexec). A new setting, *When
  GoRex's server can't run on a host* in Settings › General, decides
  what happens then:
  - Ask (the default), with the buttons Try Other Folders and Connect
    with Plain ssh.
  - Try other folders, then plain ssh.
  - Plain ssh.
- The other folders are folders you own in your runtime folder
  (`$XDG_RUNTIME_DIR`), `/var/tmp`, `/dev/shm` and `/tmp`. GoRex uses the
  first one where programs can run.
- GoRex saves what it did in the host's settings, and later connections
  use it.
- GoRex never falls back when a login fails or a host is out of reach.
- A folder that GoRex creates for its server on a host is accessible to
  you only. GoRex never runs a server from a folder that belongs to
  another user.
- `gorex ssh --plain user@host` connects with plain ssh, and `--server`
  connects with GoRex's server. Both override the host's settings.
- ssh password prompts, and the question shown when GoRex's server
  cannot run on a host, now appear above Settings and every other
  dialog.

## 0.1.9

- GoRex now connects to Linux hosts with glibc 2.17 or later, such as
  Debian 8, Ubuntu 14.04 and RHEL 7.
- The server on the host now loads GoRex's own terminal library, built
  for glibc 2.17. MyGo's library needs glibc 2.28.
- Before, no session started on a Debian 9 host (glibc 2.24). Loading
  the library failed with "version `GLIBC_2.27' not found".
- If a host's glibc is older than the server needs, GoRex copies nothing
  and starts nothing. Instead of a session failing to start, GoRex shows
  the host's glibc version and the version needed.

## 0.1.8

- Long omp transcripts draw fast. A day's session can have hundreds of
  messages and tool calls. GoRex now builds and lays out only the ones
  near the view. The others keep their last measured height until they
  change or scroll near the view.
- On a transcript of 815 messages and tool calls, a frame took 37 ms
  while scrolling (27 frames a second) and 38 ms while a command ran.
  It now takes 6 ms and 4 ms.
- Switching back to that transcript's tab took half a second. It now
  takes 4 ms.
- Resizing a window over a long omp transcript is smooth. While the
  width changes, GoRex lays out again only what is near the view, and
  draws the transcript from its end. When the width stops changing,
  GoRex measures the rest again, a few messages per frame.
- On the same transcript, a frame during the drag took 157 ms on average
  and up to half a second. It now takes 8 ms, and 13 ms per frame in the
  second after the drag.
- Fixed: after you made a window narrower and then wider again, a blank
  band could appear between the end of omp's transcript and the
  composer, or the end was out of view. The transcript now ends right
  above the composer.
- Fixed: after you left omp from its composer (for example with Ctrl+C
  twice), the shell received no keys until you clicked. The terminal
  now gets the keys back.
- Desktop notifications are now off unless you turn on Show desktop
  notifications in Settings. This covers notifications for long
  commands, for program status, and those that programs send.
- On Windows, each notification added an icon to the notification area,
  one for each running GoRex, and the icon could stay there.
- omp's BTW history is easier to read. The answer now uses all the space
  next to the list of questions. Before, it was a strip one word wide.
- GoRex now follows the sizes that programs set on their elements:
  shrinking, minimum and maximum width and height, and row alignment.
- Page Up, Page Down, Home and End in the answer scroll the sheet, not
  the terminal under it.
- A list's selected item no longer locks the scroll position of its
  container.
- An overlay with panes side by side, such as the BTW history's questions
  beside the answer, is now a sheet with its own height. Each pane
  scrolls separately, and the title and buttons stay in view.

## 0.1.7

- omp's transcript scrolls with the keyboard, as in Tern:
  - Page Up and Page Down scroll by a page.
  - Ctrl+Home and Ctrl+End go to the start and the end.
  - These work whether or not the focus is in the composer.
  - Home and End also work outside the composer. Inside it, they move
    the caret.
- Before, these keys went to omp, which does not scroll its own
  transcript.
- The composer now gets the keyboard focus when you click the dock or
  omp's controls, or switch to the pane. Clicking in the transcript
  leaves the focus where it is.
- While omp shows a picker or a sheet, the keys still go to omp.
- omp's composer now stays at the bottom of the pane when you scroll back
  past where omp started, as in Tern. Before, it disappeared while it
  had the focus, and no key scrolled back down.

## 0.1.6

- omp's dock now floats over its transcript, as in Tern. It is centered
  and at most 800 DIPs wide.
- The composer is a card. The context meter is a thin line along its top
  edge, filled to the share of the context window that is used. The
  share is shown above the end of the fill, and the window's size above
  the right end of the line. A glow moves along it while a turn runs.
- The working row sits above the card. It shows a star, the time and
  the label on one line, and the todo list as a pill at the right.
  Click the pill to see the whole plan.
- Inside the card: the session's title as the input's placeholder, the
  model as a pill, the rate, the pane's folder and the git branch on one
  line, the cost, and a round send or stop button.
- Empty parts of a program's surface no longer take space. Before, a
  band of blank space sat above the context meter.
- Fixed: stray text that a program printed under its open surface showed
  at the right edge, in the space kept clear for the scroll bar. GoRex
  now erases it.
- Settings has a new OMP Settings page, for programs that speak the Tern
  Surface Protocol, such as omp. It contains:
  - The switch that turns the protocol on. It moved from the Terminal
    page. You can now set it in the Defaults for every host, as well as
    for one host.
  - The font, size and line height of the program's text.
  - The font, size and ligatures of its code: commands and output,
    diffs, code blocks and paths. These follow the terminal's settings
    until you set them.
  - The maximum widths of the composer and the messages.
  - The spacing.
  - Whether the working indicators are animated.
- If you had turned the protocol on for a host on the Terminal page, that
  setting carries over.
- omp's slash commands and other lists at the composer's caret now open
  above the composer, as wide as the composer. Before, they were squeezed
  under the input, inside the composer.
- A list anchored somewhere else opens on the side with more room when
  its own side has too little.
- In omp's composer, Ctrl+C goes to omp unless text is selected there.
  So pressing Ctrl+C twice exits omp. When text is selected, Ctrl+C
  copies it, following the terminal's Ctrl+C setting.
- Alt with a letter or digit also goes to omp, so Alt+P switches the
  model. On macOS, Option does the same when Option is set to act as Alt.

## 0.1.5

- `gorex ssh -A user@host` forwards the ssh agent to the host for that
  window, and `-a` turns forwarding off, like ssh's own -A and -a. Both
  override the host's Forward ssh agent switch and `~/.ssh/config`.

## 0.1.4

- ssh agent forwarding now works with sessions that keep running. It
  applies when `~/.ssh/config` says `ForwardAgent yes`, or when the
  host's new Forward ssh agent switch (Settings › Hosts) is on.
- The host's sessions use the agent of the window that connected most
  recently. The agent's path stays the same, also after a reconnect.
  Before, sessions used the first connection's agent, which disappeared
  with that connection.
- When a host's GoRex server does not start after it was copied there,
  the Connection Log now shows why:
  - there is no server for the host's system;
  - the server is missing; or
  - the server cannot run. The log shows its files' modes and the mount
    it is on (noexec).
- The log also shows where the copy went, or that a copy was already
  there.
- Connecting then stops and offers Retry. Before, GoRex tried again by
  itself and asked for a passphrase each time.
- If the server already in that folder cannot run, GoRex replaces it
  with a new copy.

## 0.1.3

- Fixed: the file browser marked no files as changed in a git repository
  reached through a link. Examples are macOS's /tmp, /var and /etc, or
  a project folder that links somewhere else. It now marks the files git
  sees as changed.

## 0.1.2

- A pane now scrolls as one document, with one scroll bar: the
  terminal's. A program's surface, such as omp's transcript, takes up
  rows in the output like any other text. A long surface is shown whole,
  and you can scroll to its end.
- The scrollback setting now counts lines at any pane width, up to
  50,000. Before, wide panes kept only part of the lines.
- A transcript longer than the scrollback loses its start, as output
  does, never its end.
- The terminal's scroll bar is now the same as everywhere else in the
  window. It is 10 DIPs wide, and the same when hovered and dragged.
- It shows while the pointer is over the pane or the view is scrolled
  up. A click on its track scrolls one page.
- Surfaces and omp's composer leave the scroll bar's strip clear at the
  pane's edge.
- New settings in Settings › Defaults › Appearance, for the terminal and
  everything else that scrolls:
  - Scroll bar width.
  - Minimum scroll handle length, 48 DIPs by default, so that the handle
    of a long scrollback is not a thin sliver.
  - Always show scroll bars.
- New command: `gorex doctor [user@host]`. It shows which ssh on the
  PATH GoRex runs, and whether the ssh agent holds keys.
- For a host, `gorex doctor` makes a connection the way GoRex does. If it
  fails, it explains why in words, for example a host that wants a
  password, or keys that the host refuses.
- `gorex help` lists the commands.
- On Windows, these commands come from gorex.com, a console program
  next to GoRex.exe in the installed app's bin folder. When you type
  `gorex` in a shell, the shell runs gorex.com, waits for it and gets its
  exit status. For commands that open a window, gorex.com starts
  GoRex.exe.
- ssh now asks its questions in the window, as it would in a terminal:
  a password, a key's passphrase, a one-time code, or whether to trust a
  host key it has not seen. Before, connecting failed instead.
- Answers last for one connection. A reconnect asks again.
- Connecting stops and offers Retry when you cancel a login, when ssh
  stops asking for a password, or when a host key is not trusted or has
  changed. GoRex no longer tries again by itself.

## 0.1.1

- Programs that speak the Tern Surface Protocol, such as the coding
  agent omp, now draw natively. Their messages, tool calls, pickers,
  settings, forms, pictures, math and composer appear as GoRex controls,
  in the program's colors. They sit among the terminal's output and are
  kept in its scrollback.
- To turn this on for a host, use Settings › Programs › Tern Surface
  Protocol (experimental). omp then draws natively by itself.
- A new icon: a cyan rex on a dark tile.

## 0.1.0

The first release from gorex.goclaw.org.

- Tabs and split panes whose sessions keep running after you quit
  GoRex. A separate session server keeps the sessions.
- Each window draws a session from the server's copy of the terminal,
  so a resize or a reattach never garbles the screen.
- Session servers on other hosts over ssh. GoRex copies its server to the
  host and reconnects when the link drops.
- An editor with a file tree, git changes and a Markdown preview.
- Settings for the app and for each host.
- Images through the Kitty graphics protocol.
- Pasting images into programs such as coding agents.
- Program status (OSC 7501).
- The Windows installer puts a `gorex` command on the PATH.
- GoRex updates itself from gorex.goclaw.org. Use Check for Updates… in
  the command palette.
