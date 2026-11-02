// jarvis: the daemon, and a cli that sends it one command over its socket
package main

import (
	"bufio"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"jarvis/internal/config"
	"jarvis/internal/daemon"
)

const usage = `usage: jarvis <command>

  daemon [-config path]   run the assistant (the systemd user service does)
  listen                  listen now, prints the transcript and the answer
  pause                   close the mic, wake word too
  resume                  open it again
  stop                    stop listening or speaking
  new                     a new chat: the conversation so far is forgotten
  unload                  free the vram: the llm and the voices go, and
                          the conversation too
  clones [small|big]      the model of the cloned voices (jarvis, glados,
                          hal 9000, mayuri): 0.6b quicker, 1.7b closer
  llm [model]             the llm the characters think with: the list,
                          or pick one of it
  stt [model]             the speech to text model: the list, or pick one
  last                    print the last answer
  check                   say what keeps jarvis from working
`

func socketPath() string {
	dir := os.Getenv("XDG_RUNTIME_DIR")
	if dir == "" {
		dir = fmt.Sprintf("/run/user/%d", os.Getuid())
	}
	return filepath.Join(dir, "jarvis.sock")
}

func main() {
	log.SetFlags(0)
	if len(os.Args) < 2 {
		fmt.Fprint(os.Stderr, usage)
		os.Exit(2)
	}
	switch os.Args[1] {
	case "daemon":
		runDaemon(os.Args[2:])
	case "listen":
		os.Exit(listen())
	case "pause", "resume", "stop", "unload":
		os.Exit(send(map[string]any{"type": os.Args[1]}, nil))
	case "last":
		os.Exit(send(map[string]any{"type": "last_answer"}, func(m map[string]any) bool {
			if m["type"] != "answer" {
				return false
			}
			fmt.Println(m["text"])
			return true
		}))
	case "check":
		os.Exit(check())
	case "new":
		os.Exit(send(map[string]any{"type": "new_chat"}, nil))
	case "clones":
		os.Exit(clones(os.Args[2:]))
	case "llm":
		os.Exit(choice("llms", "set_llm", os.Args[2:]))
	case "stt":
		os.Exit(choice("stts", "set_stt", os.Args[2:]))
	case "-h", "--help", "help":
		fmt.Print(usage)
	default:
		fmt.Fprint(os.Stderr, usage)
		os.Exit(2)
	}
}

func runDaemon(args []string) {
	fs := flag.NewFlagSet("daemon", flag.ExitOnError)
	path := fs.String("config", envOr("JARVIS_CONFIG", config.DefaultPath), "config file")
	fs.Parse(args)
	cfg, err := config.Load(*path)
	if err != nil {
		log.Fatal(err)
	}
	d, err := daemon.New(cfg)
	if err != nil {
		log.Fatal(err)
	}
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()
	if err := d.Run(ctx, socketPath()); err != nil {
		log.Fatal(err)
	}
}

func envOr(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func dial() (net.Conn, *bufio.Scanner, error) {
	conn, err := net.Dial("unix", socketPath())
	if err != nil {
		return nil, nil, fmt.Errorf("jarvis is off: no daemon on %s", socketPath())
	}
	sc := bufio.NewScanner(conn)
	sc.Buffer(make([]byte, 64*1024), 1024*1024)
	return conn, sc, nil
}

// send one command. reply reads the daemon's messages until it says done,
// nil sends and leaves
func send(cmd map[string]any, reply func(map[string]any) bool) int {
	conn, sc, err := dial()
	if err != nil {
		log.Print(err)
		return 1
	}
	defer conn.Close()
	cmd["cli"] = true
	b, _ := json.Marshal(cmd)
	if _, err := conn.Write(append(b, '\n')); err != nil {
		log.Print(err)
		return 1
	}
	if reply == nil {
		return 0
	}
	conn.SetReadDeadline(time.Now().Add(30 * time.Second))
	for sc.Scan() {
		var m map[string]any
		if json.Unmarshal(sc.Bytes(), &m) != nil {
			continue
		}
		if reply(m) {
			return 0
		}
	}
	log.Print("no answer from the daemon")
	return 1
}

// listen and follow the request until it's back to idle
func listen() int {
	conn, sc, err := dial()
	if err != nil {
		log.Print(err)
		return 1
	}
	defer conn.Close()
	conn.Write([]byte(`{"type":"listen","cli":true}` + "\n"))
	started := false
	code := 0
	for sc.Scan() {
		var m map[string]any
		if json.Unmarshal(sc.Bytes(), &m) != nil {
			continue
		}
		switch m["type"] {
		case "hello":
			if s := m["state"]; s != "idle" && s != "error" {
				log.Printf("busy (%s), ignored", s)
				return 1
			}
		case "state":
			s := m["state"]
			if s != "idle" && s != "error" {
				if !started {
					started = true
				}
				fmt.Fprintf(os.Stderr, "· %s\n", s)
			} else if started {
				return code
			}
		case "transcript":
			fmt.Printf("you: %s\n", m["text"])
		case "answer":
			fmt.Printf("%s\n", m["text"])
		case "error":
			log.Printf("error: %s: %s", m["code"], m["message"])
			code = 1
			if !started {
				return 1
			}
		}
	}
	return code
}

func check() int {
	ok := false
	code := send(map[string]any{"type": "check"}, func(m map[string]any) bool {
		if m["type"] != "check" {
			return false
		}
		problems, _ := m["problems"].([]any)
		for _, p := range problems {
			fmt.Println("✗", p)
		}
		if len(problems) == 0 {
			fmt.Println("all good")
			ok = true
		}
		return true
	})
	if code != 0 || !ok {
		return 1
	}
	return 0
}

// clones: print the cloned voices' model, or pick it first
func clones(args []string) int {
	// the daemon says which on connect, watch only asks for nothing
	cmd := map[string]any{"type": "watch"}
	if len(args) > 0 {
		if args[0] != "small" && args[0] != "big" {
			log.Print("jarvis clones small or jarvis clones big")
			return 2
		}
		cmd = map[string]any{"type": "set_clones", "id": args[0]}
	}
	return send(cmd, func(m map[string]any) bool {
		if m["type"] != "clones" {
			return false
		}
		if m["small_model"] == "" {
			fmt.Println("big (the small model isn't downloaded: run ./update-models tts-base-small in the jarvis repo)")
			return true
		}
		if len(args) > 0 && m["size"] != args[0] {
			return false
		}
		fmt.Println(m["size"])
		return true
	})
}

// choice: a list to pick from (the llms, the stt models), the picked one
// marked, or pick one first
func choice(list, set string, args []string) int {
	code := 0
	cmd := map[string]any{"type": "watch"}
	if len(args) > 0 {
		cmd = map[string]any{"type": set, "id": args[0]}
	}
	sent := send(cmd, func(m map[string]any) bool {
		if m["type"] != list {
			return false
		}
		list, _ := m["list"].([]any)
		if len(args) > 0 && m["selected"] != args[0] {
			known := false
			for _, x := range list {
				e, _ := x.(map[string]any)
				known = known || e["id"] == args[0]
			}
			if known {
				// the list from before the pick, the next one has it
				return false
			}
			log.Printf("no %q, these are:", args[0])
			code = 2
		}
		for _, x := range list {
			e, _ := x.(map[string]any)
			mark := "  "
			if e["id"] == m["selected"] {
				mark = "* "
			}
			fmt.Printf("%s%v  %v\n", mark, e["id"], e["name"])
		}
		return true
	})
	if sent != 0 {
		return sent
	}
	return code
}
