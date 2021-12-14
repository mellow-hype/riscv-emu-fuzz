package main

import (
	"fmt"
)

// Terminal colors
var Reset = "\033[0m"
var Red = "\033[31m"
var Green = "\033[32m"
var Yellow = "\033[33m"
var Blue = "\033[34m"
var Purple = "\033[35m"
var Cyan = "\033[36m"
var Gray = "\033[37m"
var White = "\033[97m"

var ActiveColor = Reset

func PrintCl(color string, message string) {
	print(color)
	fmt.Println(message)
	print(ActiveColor)
}

func SetColor(color string) {
	ActiveColor = color
	print(color)
}

func ResetColor() {
	print(Reset)
}

func PrintDbg(message string, a ...interface{}) {
	print(Yellow)
	fmt.Printf("[DBG]: ")
	colored_msg := fmt.Sprintf(message, a...)
	fmt.Println(colored_msg)
	SetColor(ActiveColor)
}

// Return the calling function's name
// func currentFunc() string {
// 	pc := make([]uintptr, 15)
// 	n := runtime.Callers(2, pc)
// 	frames := runtime.CallersFrames(pc[:n])
// 	frame, _ := frames.Next()
// 	return frame.Function
// }
