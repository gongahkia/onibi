package main

import (
	"bufio"
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"
)

type Directory struct {
	Files        []int
	Subdirs      map[string]*Directory
	TotalSize    int
}

func main() {
	fmt.Println("day 7 solution")
	partA("input.txt")
}

func partA(fileName string) {
	root := &Directory{
		Subdirs: make(map[string]*Directory),
	}
	dirMap := map[string]*Directory{
		"/": root,
	}

	currDir := root
	currPath := []string{"/"}

	// File handling
	fhand, err := os.Open(fileName)
	if err != nil {
		log.Println("error reading file:", err)
		return
	}
	defer fhand.Close()

	scanner := bufio.NewScanner(fhand)
	for scanner.Scan() {
		line := scanner.Text()
		sanitisedA := strings.TrimSpace(line)

		if strings.HasPrefix(sanitisedA, "$") {
			// Handle commands
			command := strings.Fields(sanitisedA)
			switch command[1] {
			case "cd":
				if command[2] == "/" {
					currDir = root
					currPath = []string{"/"}
				} else if command[2] == ".." {
					if len(currPath) > 1 {
						currPath = currPath[:len(currPath)-1]
						currDir = dirMap[strings.Join(currPath, "/")]
					}
				} else {
					currPath = append(currPath, command[2])
					newPath := strings.Join(currPath, "/")
					if _, exists := dirMap[newPath]; !exists {
						dirMap[newPath] = &Directory{
							Subdirs: make(map[string]*Directory),
						}
					}
					currDir = dirMap[newPath]
				}
			case "ls":
				// Ignore
			}
		} else {
			// Handle file/directory listings
			entry := strings.Fields(sanitisedA)
			if entry[0] == "dir" {
				newPath := strings.Join(append(currPath, entry[1]), "/")
				if _, exists := dirMap[newPath]; !exists {
					dirMap[newPath] = &Directory{
						Subdirs: make(map[string]*Directory),
					}
				}
				currDir.Subdirs[entry[1]] = dirMap[newPath]
			} else {
				fileSize, err := strconv.Atoi(entry[0])
				if err != nil {
					log.Println("error reading line:", err)
					continue
				}
				currDir.Files = append(currDir.Files, fileSize)
			}
		}
	}

	if err := scanner.Err(); err != nil {
		log.Println("error scanning file:", err)
	}

	// Compute total sizes
	computeTotalSizes(root)

	// Calculate space requirements
	const totalDiskSpace = 70000000
	const requiredUnusedSpace = 30000000

	totalUsedSpace := root.TotalSize
	currentUnusedSpace := totalDiskSpace - totalUsedSpace
	neededFreeSpace := requiredUnusedSpace - currentUnusedSpace

	// Find smallest directory to delete
	smallestDirSize := totalDiskSpace // Initialize with a large number
	for _, dir := range dirMap {
		if dir.TotalSize >= neededFreeSpace && dir.TotalSize < smallestDirSize {
			smallestDirSize = dir.TotalSize
		}
	}
	fmt.Println("Smallest directory size to delete:", smallestDirSize)
}

func computeTotalSizes(dir *Directory) int {
	for _, size := range dir.Files {
		dir.TotalSize += size
	}
	for _, subdir := range dir.Subdirs {
		dir.TotalSize += computeTotalSizes(subdir)
	}
	return dir.TotalSize
}
