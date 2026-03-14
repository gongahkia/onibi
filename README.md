![](https://img.shields.io/badge/kelp_1.0-passing-green)

# KELP 🌿🌊

The *K*ommand line h*elp*er.

Written in Rust, because [we love crabs](https://www.reddit.com/r/rust/comments/uboyeq/why_is_rust_the_most_loved_programming_language/?rdt=50321).

Installation handled in Bash, because making *Kelp* felt like [bashing my head in](https://www.reddit.com/r/rust/comments/cgs9lj/why_do_people_hate_rust/).

## Feature roadmap

| Feature implementation | Implemented |
| :---: | :---: |
| **To-do list** | ~ |
| **Task attributes** | ~  |
| Task name | ✅ |
| Task description | ✅ |
| Task due date | ✅ |
| Task urgency | ✅ |
| Task tags | ~  |
| Tags to be attached to each task optionally, to sort tasks by tags | ✅ |
| **Data storage** | ~ |
| Changes written to local file titled `.kelpStorage` | ✅ |
| Loads saves from `.kelpStorage` | ✅ |
| Online sync via Google drive Api | *Implemented in Kelp 2.0* |
| **Create task** | ~ |
| Error handling | ✅ |
| **Completed tasks** | ~ |
| Check off completed tasks | ✅ |
| **Editing tasks** | ~ |
| Task name | ✅ |
| Task description | ✅ |
| Task deadline | ✅ |
| Task urgency | ✅ |
| Task tags | ✅ |
| **Sorting tasks** | ~ |
| Due date | ✅ |
| Urgency level | ✅ |
| Tags | ✅ |
| **Deleting tasks** | ~ |
| Recycle bin stores deleted tasks for 30 days | *Implemented in Kelp 2.0* |
| Completed tasks saved until entire list is deleted | *Implemented in Kelp 2.0* |
| **List creation** | ~ |
| Lighter-weight alternative to projects that sorts related tasks together. | ✅ |
| **Project management** | ~ |
| Each project comprises an *entire task list*. | ✅ |
| Project name | ✅ |
| Project tasks | ✅ |
| Project completion percentage | *Implemented in Kelp 2.0* |
| **Quality of life improvements** | ~ |
| Program commands can optionally be interacted with via cli commands *(similar to `neofetch` and `git`)*. | *Implemented in Kelp 2.0* |
| Kanban board functionality that can be visualised in CLI. | *Implemented in Kelp 2.0* |
| Sane defaults and keymaps | *Implemented in Kelp 2.0* |
| **Program installation** | ~ |
| Handle all program and dependancy installation with bash scripting. | ✅ |
| Bash script edits `.bashrc` file to add aliases and path for CLI commands to be used. | ✅ |
| Port program to Linux, Windows and Mac environments. | ✅ |
| Rebuild this as a webapp using rust's iced library and tauri. | *Implemented in Kelp 3.0* |

---

## Deployment

| Platform | Status | Download |
| :---: | :---: | :---: |
| Windows | Up | On WSL, below instructions |
| MacOS | Up | Below instructions |
| Linux | Up | Below instructions |

---

## Dependancies

* `curl`
* `wget`
* `git`

## Installation and usage

1. Run the following commands in your terminal.

```console
$ wget https://raw.githubusercontent.com/gongahkia/Kelp/main/installer.sh
$ chmod +x installer.sh
$ ./installer.sh
```

2. After running the Rust installer, we have to add a line of code to the **bottom** of our `.bashrc` file to indicate the file path. Remember to **source** your `.bashrc` file. (Neovim is used below, but any other code editor can be used).

```console
$ nvim ~/.bashrc
$ source ~/.bashrc
```

*Line to be added:*

```bash
export PATH=~/.config/Kelp-build:$PATH
```

3. Finally, `cd` back into the directory that we previously ran the `installation.sh` binary in, and remove the installation files.

```console
$ rm -r installer.sh Kelp
```

---

## Uninstalling Kelp

```console
$ cd ~/.config
$ rm -r Kelp-build
```

Additionally, remember to remove the line added to your `.bashrc` file.

```console
$ nvim ~/.bashrc
-- removes final line from file
```
