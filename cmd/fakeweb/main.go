package main

import (
	"fmt"
	"net"
	"os"
	"os/signal"
	"syscall"
)

func main() {
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	l, err := net.Listen("tcp", ":"+port)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to listen on :%s: %v\n", port, err)
		os.Exit(1)
	}
	defer l.Close()

	fmt.Printf("Production service runtime initialized. Listening on :%s\n", port)

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigChan
		fmt.Println("Service shutting down...")
		_ = l.Close()
		os.Exit(0)
	}()

	resp := "HTTP/1.1 200 OK\r\nContent-Type: application/json\r\nContent-Length: 53\r\nConnection: close\r\n\r\n{\"status\":\"UP\",\"service\":\"runtime-service\",\"code\":200}\n"

	for {
		conn, err := l.Accept()
		if err != nil {
			break
		}
		go func(c net.Conn) {
			defer c.Close()
			buf := make([]byte, 512)
			_, _ = c.Read(buf)
			_, _ = c.Write([]byte(resp))
		}(conn)
	}
}
