# KELP 🌿🌊

The *K*ommand line h*elp*er.

Written in Rust, because we love crabs.

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
| Lighter-weight alternative to projects that sorts related tasks together. | *Implemented in Kelp 2.0* |
| **Project management** | ~ |
| Each project comprises an *entire task list*. | *Implemented in Kelp 2.0* |
| Project name | *Implemented in Kelp 2.0* |
| Project description | *Implemented in Kelp 2.0* |
| Project tasks | *Implemented in Kelp 2.0* |
| Project completion percentage | *Implemented in Kelp 2.0* |
| **Quality of life improvements** | ~ |
| Program commands can optionally be interacted with via cli commands *(similar to `neofetch` and `git`)*. | *Implemented in Kelp 2.0* |
| Kanban board functionality that can be visualised in CLI. | *Implemented in Kelp 2.0* |
| Sane defaults and keymaps | *Implemented in Kelp 2.0* |
| **Program installation** | ~ |
| Handle all program and dependancy installation with bash scripting. | *Implemented in Kelp 2.0* |
| Bash script edits `.bashrc` file to add aliases and path for CLI commands to be used. | *Implemented in Kelp 2.0* |
| Port program to Linux, Windows and Mac environments. | *Implemented in Kelp 2.0* |
| Rebuild this as a webapp using rust's iced library and tauri. | *Implemented in Kelp 3.0* |

---

## Deployment 

| Platform | Status | Download |
| :---: | :---: | :---: |
| Windows | Up | On WSL, below instructions |
| MacOS | Up | Below instructions |
| Linux | Up | Below instructions |

---

## Installation and usage

1. Run the following commands in your terminal.

> To continue from here, adding instructions to curl the installation file and run ti

```console
$ 
$ chmod +x installer.sh
$ ./installer.sh
```

2. After running the Rust installer, we have to add a line of code to the **bottom** of our `.bashrc` file to indicate the file path. *(Neovim is used below, but it can be replaced with any other code editor)*.

```console
$ nvim ~/.bashrc
```

*Line to be added:* 

```bash
export PATH=~/.config/Kelp-build:$PATH
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
