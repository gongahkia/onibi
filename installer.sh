#!/bin/bash

RED="\e[31m"
GREEN="\e[32m"
YELLOW="\e[33m"
BLUE="\e[34m"
GRAY="\e[90m"
MAGENTA="\e[95m"
CYAN="\e[96m"
ENDCOLOR="\e[0m"

clear
printf "Welcome to the ${GREEN}Kelp installer.${ENDCOLOR}\n"
printf "${YELLOW}Beginning the install in 5 seconds...${ENDCOLOR}\n"

sleep 5
clear
printf "${YELLOW}Detecting OS...${ENDCOLOR}\n"
sleep 2
printf "${GREEN}OS detected!${ENDCOLOR}\n"

function linuxDistro() {
    if [[ -f /etc/os-release ]]
    then
        source /etc/os-release
        echo $ID
    fi
}

# Rust installs on...

if [[ $OSTYPE == darwin ]]; then
    # OSX
    printf "OS: ${BLUE}MacOS${ENDCOLOR}\n"
    sleep 2
    printf "${YELLOW}Proceeding with Rust installation.${ENDCOLOR}\n"
    sleep 2
    curl https://sh.rustup.rs -sSf | sh -s -- --help

elif [[ $OSTYPE == linux-gnu ]]; then
    # Linux
    printf "OS: ${BLUE}Linux generic install${ENDCOLOR}\n"
    sleep 2
    printf "${YELLOW}Proceeding with Rust installation.${ENDCOLOR}\n"
    sleep 2
    curl https://sh.rustup.rs -sSf | sh -s -- --help

elif [[ $OSTYPE == msys ]]; then
    # WSL
    printf "OS: ${BLUE}Windows${ENDCOLOR}\n"
    sleep 2
    printf "${YELLOW}Proceeding with Rust installation.${ENDCOLOR}\n"
    sleep 2
    curl --proto '=https' --tlsv1.2 -sSf https://sh.rustup.rs | sh

elif [[ $OSTYPE == cygwin ]]; then
    # WSL
    printf "OS: ${BLUE}Windows${ENDCOLOR}\n"
    sleep 2
    printf "${YELLOW}Proceeding with Rust installation.${ENDCOLOR}\n"
    sleep 2
    curl --proto '=https' --tlsv1.2 -sSf https://sh.rustup.rs | sh

else
    # undetected OS
    echo $OSTYPE
    printf "${RED}OS cannot be detected. Defaulting to Rust install for WSL.${ENDCOLOR}\n"
    sleep 2
    printf "${YELLOW}Proceeding with Rust installation.${ENDCOLOR}\n"
    sleep 2
    curl --proto '=https' --tlsv1.2 -sSf https://sh.rustup.rs | sh

fi

# actual code
sleep 3
git clone https://github.com/gongahkia/Kelp
clear
cd Kelp
printf "${GREEN}Rust installed.${ENDCOLOR}\n"
printf "${GREEN}Git repo cloned.${ENDCOLOR}\n"
printf "${YELLOW}Building Cargo binary.${ENDCOLOR}\n"
cargo build --release
clear
printf "${GREEN}Cargo Binary built.${ENDCOLOR}\n"
printf "${YELLOW}Enter [y] to the following prompts.${ENDCOLOR}\n"
rm -r LICENSE .gitignore LearningPointers src .git Cargo.toml Cargo.lock README.md installer.sh 
cd target/release
cp kelp ~/.config
cd ~/.config
mkdir Kelp-build
mv kelp Kelp-build

# final commands
clear
printf "${BLUE}Installation completed!${ENDCOLOR}\n"
