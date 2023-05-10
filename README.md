> **THIS IS A WORK IN PROGRESS!!!**

# KELP 🌿🌊

The *K*ommand line h*elp*er.

Written in Rust, because we love crabs.

## Feature roadmap

* [ ] ***Kelp***
    * [ ] *Program*
        * [x] **To-do list**
            * [x] add a break condition to exit the program when run
        * [x] **task components**
            * [ ] tags *(likely implemented in Kelp 2.0)*
            * [x] task name
            * [x] description, details
            * [x] due date
            * [x] urgency level
        * [x] **sorting tasks**
            * [x] sort tasks by the following criteria
                * [x] due date
                * [x] urgency level
                * [ ] tags *(likely implemented in Kelp 2.0)*
        * [x] **storing data**
            * [ ] integrate online syncing to google drive via drive API *(likely implemented in Kelp 2.0)*
            * [x] changes written to a local storage file titled `.kelpStorage`
            * [x] automatically load local storage file upon entering program
        * [x] **completed tasks**
            * [x] check off completed tasks
        * [x] **editing tasks**
            * [x] allow for editing every component of a task (name, description, due date, urgency level, tags)
        * [ ] **delete tasks** *(likely implemented in Kelp 2.0)*
            * [ ] deleted tasks are stored in recycle bin for 30 days, removed from storage after
            * [ ] completed tasks saved until entire list is deleted 
        * [ ] **list creation**
            * [ ] allow for lighter-weight alternative to projects, while still sorting related tasks together
        * [ ] **tags**
            * [ ] tags to be attached to each task optionally, to sort tasks by tags
        * [ ] **project management**
            * [ ] create a project with the following components:
                * project name
                * project description
                * project completion percentage
                * project tasks
            * [ ] each project comprises an entire task list, and each task has its base components as specified above
        * [ ] **quality of life improvements**
            * [ ] all program commands can be interacted with via cli commands *(similar to `neofetch` and `git`)*
            * [ ] notification when task is overdue, due today, due tommorrow, due this week or due later than this week
            * [ ] add kanban board functionality that can be visualised in CLI
            * [ ] figure out sane defaults and keymaps
    * [ ] *Program installation*
        * [ ] handle all program and dependancy installation with bash scripting *(similar to my `gitfetch` program)*
        * [ ] add bash script that edits `.bashrc` file to add aliases and path for CLI commands to be used
        * [ ] work to port over program to Linux, Windows and Mac environments
        * [ ] rebuild this as a webapp using rust's iced library and tauri

## Deployment 

| Platform | Status | Download |
| :---: | :---: | :---: |
| Windows | | 
| MacOS | |
| Linux | |

## Installation

```console
$
```

## Usage

| Command | Keymap | Function |
| :---: | :---: | :---: |
