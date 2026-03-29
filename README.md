[![](https://img.shields.io/badge/kelp_1.0.0-passing-light_green)](https://github.com/gongahkia/kelp/releases/tag/1.0.0)
[![](https://img.shields.io/badge/kelp_2.0.0-passing-green)](https://github.com/gongahkia/kelp/releases/tag/2.0.0)

# `Kelp` 🌿🌊

The *K*ommand line h*elp*er.

## Dependencies

`Kelp` relies on [curl](https://curl.se/), [wget](https://www.gnu.org/software/wget/) and [git](https://git-scm.com/).

## Install Kelp

```console
$ curl -fsSL https://raw.githubusercontent.com/gongahkia/kelp/main/installer.sh -o installer.sh
$ chmod +x installer.sh
$ ./installer.sh --build-from-source
$ ./installer.sh --release-version 1.0.0
$ ./installer.sh --with-completions
```

After running the Rust installer, we have to add a line of code to the **bottom** of our `.bashrc` file to indicate the file path. Remember to **source** your `.bashrc` file. (Neovim is used below, but any other code editor can be used).

```console
$ nvim ~/.bashrc
$ source ~/.bashrc
```

*Line to be added:*

```bash
export PATH=~/.config/Kelp-build:$PATH
```

Finally, `cd` back into the directory that we previously ran the `installation.sh` binary in, and remove the installation files.

```console
$ rm -r installer.sh Kelp
```

## Uninstall Kelp

```console
$ cd ~/.config
$ rm -r Kelp-build
```

Additionally, remember to remove the line added to your `.bashrc` file.

```console
$ nvim ~/.bashrc
-- removes final line from file
```

## Usage

```console
$ kelp init
$ kelp project add --name Launch --deadline next-week
$ kelp task add --title "Draft release notes" --project Launch --priority high --due tomorrow
$ kelp task ready
$ kelp review daily
$ kelp review weekly
```

## Support

| Platform | Status | Download |
| :---: | :---: | :---: |
| Windows | Up | On WSL, below instructions |
| MacOS | Up | Below instructions |
| Linux | Up | Below instructions |

## 2 puns

1. `Kelp` was written in Rust because [we love crabs](https://www.reddit.com/r/rust/comments/uboyeq/why_is_rust_the_most_loved_programming_language/?rdt=50321).
2. Installation for `Kelp` was handled in Bash, because making `Kelp` felt like [bashing my head in](https://www.reddit.com/r/rust/comments/cgs9lj/why_do_people_hate_rust/).
