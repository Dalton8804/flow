package main

import (
	"bufio"
	"flag"
	"fmt"
	"log"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/fsnotify/fsnotify"
)

func checkDirectory(directory string) {
	if directory == "" {
		fmt.Println("Directory not specified. Use the -d flag to specify a directory to watch/sync.")
		os.Exit(0)
	}

	dirInfo, err := os.Stat(directory)
	if os.IsNotExist(err) {
		fmt.Printf("Directory %s does not exist.", directory)
		os.Exit(0)
	}
	if dirInfo.IsDir() == false {
		fmt.Printf("%s is not a directory.", directory)
		os.Exit(0)
	}
}

func main() {
	var directory string
	var room_code string
	flag.StringVar(&directory, "d", "", "Directory to watch/sync")
	flag.StringVar(&room_code, "r", "", "Room code to join")

	flag.Parse()

	checkDirectory(directory)

	conn, err := net.Dial("tcp", "localhost:12345")
	if err != nil {
		fmt.Println("Error connecting to server: ", err)
		fmt.Println("Ensure use of command is correct: run `flow --help` for more information")
		fmt.Println("Check server status if problem continues.")
		return
	}
	defer conn.Close()

	interrupt := make(chan os.Signal, 1)
	signal.Notify(interrupt, os.Interrupt, syscall.SIGTERM)

	go func() {
		<-interrupt
		conn.Close()
		os.Exit(0)
	}()

	connectionIsSender := false

	if room_code == "" {
		room_code = "NEW"
		connectionIsSender = true
	}

	_, err = conn.Write([]byte(room_code))
	if err != nil {
		fmt.Println("Error sending initial code:", err)
		return
	}

	for {
		if connectionIsSender {
			monitorDirectory(directory, conn)
		} else {
			receiveChanges(directory, conn)
		}
	}
}

func watchForMessage(conn net.Conn, responseChannel chan []byte) {
	for {
		buf := make([]byte, 1024)
		n, err := conn.Read(buf)
		if err != nil {
			fmt.Println("Connection closed by server.")
			os.Exit(0)
			break
		}
		responseChannel <- buf[:n]
	}
}

func monitorDirectory(directory string, conn net.Conn) {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		log.Fatal(err)
	}
	defer watcher.Close()

	err = createWatchers(directory, watcher)
	if err != nil {
		log.Fatal(err)
	}

	responseChannel := make(chan []byte)

	go watchForMessage(conn, responseChannel)

	for {
		select {
		case event := <-watcher.Events:
			if event.Op&fsnotify.Create == fsnotify.Create {
				fmt.Println("Created file:", event.Name)
				sendFile(conn, event.Name)
			} else if event.Op&fsnotify.Write == fsnotify.Write {
				fmt.Println("Modified file:", event.Name)
				sendFile(conn, event.Name)
			} else if event.Op&fsnotify.Remove == fsnotify.Remove {
				fmt.Println("Removed file:", event.Name)
				sendFile(conn, event.Name)
			}
		case err := <-watcher.Errors:
			log.Println("Error:", err)
		case response := <-responseChannel:
			fmt.Println(string(response))
		}
	}
}

func createWatchers(directory string, watcher *fsnotify.Watcher) error {
	err := filepath.Walk(directory, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			// Add the directory to the watcher
			toIgnore, _ := loadFlowIgnore()
			for _, ignore := range toIgnore {
				if strings.HasPrefix(path, ignore) {
					return nil
				}
			}
			fmt.Println("Adding directory:", path)
			if err := watcher.Add(path); err != nil {
				return err
			}
		}
		return nil
	})
	return err
}

// TODO: Receive changes should replace files
func receiveChanges(directory string, conn net.Conn) {
	for {
		buf := make([]byte, 1024)
		n, err := conn.Read(buf)
		if err != nil {
			fmt.Println("Connection closed by server.")
			os.Exit(0)
			break
		}
		fmt.Println(string(buf[:n]))
	}
}

// TODO: Send file to relay server
func sendFile(conn net.Conn, filename string) {
	file, err := os.Open(filename)
	if err != nil {
		fmt.Println("Error opening file:", err)
		return
	}
	defer file.Close()

	_, err = file.Stat()
	if err != nil {
		fmt.Println("Error getting file info:", err)
		return
	}

	_, err = conn.Write([]byte(filename))
	if err != nil {
		fmt.Println("Error sending file name:", err)
		return
	}

	buf := make([]byte, 1024)
	for {
		n, err := file.Read(buf)
		if err != nil {
			break
		}
		_, err = conn.Write(buf[:n])
		if err != nil {
			fmt.Println("Error sending file:", err)
			return
		}
	}
	fmt.Printf("sent %s successfully", filename)
}

func loadFlowIgnore() ([]string, error) {
	file, err := os.Open(".flowignore")
	if err != nil {
		return nil, err
	}
	defer file.Close()

	var lines []string
	scanner := bufio.NewScanner(file)

	for scanner.Scan() {
		line := scanner.Text()
		lines = append(lines, line)
	}

	if err := scanner.Err(); err != nil {
		return nil, err
	}

	return lines, nil
}
