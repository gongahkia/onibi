package main

import "fmt" // for printing
import "bufio" // for reading input output from buffer
import "os" // for calling os commands
import "strings" // for handling strings

func main(){

	fhand, err := os.Open("./input-1.txt")

	if err == nil {
		scanner := bufio.NewScanner(fhand)	
		for scanner.Scan(){
			curStr := scanner.Text()
			fmt.Println(curStr)
			curSrr
			/*
			for index, char := range curStr {
				fmt.Printf("Index: %d and its associated char: %c\n", index, char)
				// now we got each char
			}
			*/
		}
	} else {}

	// fmt.Println("Hello World")

}
