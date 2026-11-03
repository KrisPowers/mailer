# Webmail

The webmail is where accounts read and write their mail in a browser, so no
other mail app is needed. It's at <http://localhost:8081> on the computer running
Docker, and at `<address>.<primary domain>` (default `webmail`) when the admin
serves it from anywhere (Settings, Server, in the admin dashboard). An account
signs in with its address and password.

Outlook, Thunderbird and phone mail apps work beside it, for accounts whose
**Mail apps** switch is on. It's off for new accounts; an admin turns it on in the
account's page in the admin dashboard.

## Reading

The sidebar has **New message**, the folders with their unread counts, **Flagged**
(flagged mail in every folder) and the categories. The list shows the open folder
newest first, with filters for unread, flagged and attachments; search (Ctrl+K)
looks through every folder.

A message opens beside the list. HTML mail is shown in a sandboxed frame where
nothing runs, and pictures from the internet stay hidden until **Show pictures**,
so a sender can't tell it was opened (Settings, General, can show them always).
The message says whether its sender passed SPF, DKIM and DMARC, why it was filed as
junk, and warns of attachments that can run code.

Pick messages with their boxes to mark them read or unread, flag them, give them a
category, move them, mark them as junk or delete them, several at once. Messages
can also be dragged onto a folder. Delete moves to Trash; deleting in Trash, or
emptying it, is for good.

## Writing

Reply, Reply all and Forward start from the message, quoting it; a forward keeps
its attachments. Messages are written with bold, italics, lists, quotes and links,
take attachments (pick them or drop them in), and suggest addresses from the
people written to and heard from most. **Cc Bcc** opens those lines. An account
with aliases picks which address it sends from.

What's being written saves to Drafts every minute and when it's closed; opening a
draft carries on with it. Sent mail is filed in Sent.

## Settings

| Section | What's there |
|---|---|
| **General** | Your name on what you send, marking messages read when opened, showing pictures from the internet |
| **Signatures** | Signatures, and which goes on new messages and which on replies and forwards. Any one can be put in from the writing toolbar |
| **Categories** | Colour labels, named and coloured as you like. They're kept on the message as the keyword `$cat-<id>`, so mail apps keep them |
| **Rules** | Rules file mail as it arrives, from the top: when all (or any) of their tests match the sender, recipients, subject, text, an attachment or bulk mail, they move or copy it to a folder, mark it read, flag it, give it a category or delete it, and can stop the rules under them. **Run now** files what's in the Inbox already |
| **Folders** | Rename and remove folders (their mail goes to Trash) and empty Trash and Junk. A `/` in a name puts a folder inside another |
| **Appearance** | Light, dark or the system's theme, motion, and density |
| **Account** | Who's signed in, the addresses it sends as, whether its mail apps are on, and sign out |

Settings are kept with the account on the server, in the data folder's `webmail`
folder, so they're the same in every browser.

## Sessions

Signing in lasts 30 days, or two weeks unused, in an HttpOnly cookie. A new
password, or the account's sign-in being locked, ends it at once. Wrong passwords
count against the same limits as mail apps': five from one address in 15 minutes
block that address, and the admin dashboard lists webmail sign-ins beside the
apps'.
