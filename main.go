// Watch — рекурсивный файловый watcher с polling для macOS и совместимости с acme.
package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintf(os.Stderr, "Usage: %s command [args...]\n", os.Args[0])
		os.Exit(1)
	}

	cmdLine := os.Args[1:]

	// Получаем абсолютный путь к текущей директории
	cwd, err := os.Getwd()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Watch: cannot get current directory: %v\n", err)
		os.Exit(1)
	}

	// Канал для сигнализации об изменениях
	change := make(chan struct{}, 1) // буферизованный, чтобы не блокировать

	// Запускаем polling в отдельной горутине
	go pollDirectory(cwd, change)

	// Основной цикл: ждём изменений и запускаем команду
	for {
		<-change
		fmt.Fprintf(os.Stderr, "Watch: change detected, running %q\n", cmdLine)

		cmd := exec.Command(cmdLine[0], cmdLine[1:]...)
		cmd.Stdin = nil
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr

		if err := cmd.Run(); err != nil {
			fmt.Fprintf(os.Stderr, "Watch: command failed: %v\n", err)
			// Не выходим — продолжаем следить
		}

		// После запуска команды — сбрасываем и ждём следующего изменения
		// (игнорируем все изменения, произошедшие во время выполнения команды)
		for len(change) > 0 {
			<-change // сброс накопленных сигналов
		}
	}
}

// pollDirectory рекурсивно сканирует директорию и отправляет сигнал при любом изменении
func pollDirectory(root string, change chan<- struct{}) {
	// Карта: путь → mtime
	state := make(map[string]int64)

	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	for range ticker.C {
		newState := make(map[string]int64)

		// Собираем текущее состояние
		err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
			if err != nil {
				// Пропускаем недоступные файлы (например, из-за прав)
				return nil
			}
			if d.IsDir() {
				return nil
			}
			info, err := d.Info()
			if err != nil {
				return nil
			}
			newState[path] = info.ModTime().Unix()
			return nil
		})

		if err != nil {
			fmt.Fprintf(os.Stderr, "Watch: error walking directory: %v\n", err)
			continue
		}

		// Сравниваем с предыдущим состоянием
		changed := false
		for path, mtime := range newState {
			if prev, ok := state[path]; !ok || prev != mtime {
				changed = true
				break
			}
		}
		if !changed {
			for path := range state {
				if _, ok := newState[path]; !ok {
					// Файл удалён
					changed = true
					break
				}
			}
		}

		if changed {
			// Отправляем сигнал, но не блокируемся
			select {
			case change <- struct{}{}:
			default:
				// Канал уже содержит сигнал — ничего не делаем
			}
			state = newState
		}
	}
}