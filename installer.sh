#!/bin/bash

RED="\e[31m"
GREEN="\e[32m"
BLUE="\e[34m"
GRAY="\e[90m"
ENDCOLOR="\e[0m"

function linuxDistro() {
    if [[ -f /etc/os-release ]]
    then
        source /etc/os-release
        echo $ID
    fi
}

# Rust install on...

if [[ $OSTYPE == darwin ]]; then
    # OSX
    printf "OS: ${BLUE}MacOS${ENDCOLOR}\n"
    curl https://sh.rustup.rs -sSf | sh -s -- --help

elif [[ $OSTYPE == linux-gnu ]]; then
    # Linux
    printf "OS: ${BLUE}Linux generic install${ENDCOLOR}\n"
    curl https://sh.rustup.rs -sSf | sh -s -- --help

elif [[ $OSTYPE == msys ]]; then
    # WSL
    printf "OS: ${BLUE}Windows${ENDCOLOR}\n"
    curl --proto '=https' --tlsv1.2 -sSf https://sh.rustup.rs | sh

elif [[ $OSTYPE == cygwin ]]; then
    # WSL
    printf "OS: ${BLUE}Windows${ENDCOLOR}\n"
    curl --proto '=https' --tlsv1.2 -sSf https://sh.rustup.rs | sh

else
    # undetected OS
    echo $OSTYPE
    printf "${RED}OS cannot be detected${ENDCOLOR}\n"
    curl --proto '=https' --tlsv1.2 -sSf https://sh.rustup.rs | sh

fi

# actual code
git clone https://github.com/gongahkia/Kelp
cargo run
rm -r LICENSE .gitignore LearningPointers src Cargo.toml Cargo.lock README.md
cd target/release
cp kelp ~/.config/Kelp-build/

# echoing to bashrc
echo "export PATH=~/.config/Kelp-build/:$PATH" >> ~/.bashrc
echo "alias kelp='./kelp'" >> ~/.bashrc
