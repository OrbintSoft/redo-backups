# Plan — redo-backups

What comes next, and why. The goals are in [README.md](README.md#goals); the
rules of the work in [CLAUDE.md](CLAUDE.md).

**Status legend:** `[ ]` todo · `[~]` in progress · `[x]` done

---

## 1 — A backup another tool can check and start  `[~]`

Asked on 2026-11-26. A tool that runs before a system update, such as
emergup, checks that the last backup is recent enough and, when it is not,
offers to start one. That tool knows no backup format, so the check
belongs here: `redo-backup check`, whose exit status tells "too old", which a
backup fixes, from "the destination is missing", which a backup would make
worse. The caller offers to start the backup only for the first.

- [x] **1.1** `run` refuses a destination that is not available. It used to
      create the destination (`os.MkdirAll`) unchecked, so with the backup
      disk not mounted it wrote the descriptor and the images onto the
      filesystem the mount point sits on, often the root filesystem of the
      drive being backed up.
      - Decided, both, in `run` and `run --dry-run`: a destination stored on
        the drive being imaged is always refused (`findmnt --target` on its
        nearest existing ancestor, then `lsblk --inverse` through LVM, LUKS
        or RAID down to the drives); and the optional profile key
        `dest_mount` names the mount point the destination must be on, which
        also catches an unmounted backup disk while another drive is imaged.
      - Either refusal is `backup.ErrDestUnavailable`; a check that cannot
        run (`findmnt` failing, say) is a different error. A missing
        destination directory is still created, but only after the check.
- [ ] **1.2** `redo-backup check <profile> --max-age <age>`: exits with 0
      when the newest complete backup in the profile's destination is
      younger than the age, 1 when it is older or there is none, and 2 when
      the destination is not available, as 1.1 decides it. It prints the
      backup it found and its age.
      - The age comes from the descriptor's `timestamp`, not from the
        file's modification time, which a copy changes.
      - To decide: how a complete backup is told from one that failed half
        way, since the descriptor is written before the imaging; and how
        the age is written (Go durations have no days: `20d` needs a parser
        of its own).
