package main

import (
	"context"
	"os"
	"os/signal"
	"time"

	"github.com/hizkifw/hoshinova/config"
	"github.com/hizkifw/hoshinova/logger"
	"github.com/hizkifw/hoshinova/notifier"
	"github.com/hizkifw/hoshinova/recorder"
	"github.com/hizkifw/hoshinova/taskman"
	"github.com/hizkifw/hoshinova/tui"
	"github.com/hizkifw/hoshinova/uploader"
	"github.com/hizkifw/hoshinova/util"
	"github.com/hizkifw/hoshinova/watcher"
)

func main() {
	cfg, err := config.LoadConfig()
	if err != nil {
		panic(err)
	}

	lg := logger.New(logger.LogLevelDebug)
	tm := taskman.New(cfg, lg)

	// Create context
	ctx, cancel := context.WithCancel(context.Background())
	ctx = util.WithLogger(ctx, lg)
	ctx = util.WithConfig(ctx, cfg)
	ctx = util.WithTaskManager(ctx, tm)

	// Set up TUI
	go tui.New().Run(ctx, cancel)
	time.Sleep(time.Millisecond * 100)

	var uploaders []uploader.Uploader
	var notifiers []notifier.Notifier

	// Create uploaders
	for _, u := range cfg.Uploaders {
		upl, err := uploader.NewUploader(u)
		if err != nil {
			panic(err)
		}
		uploaders = append(uploaders, upl)
	}

	// Create notifiers
	for _, n := range cfg.Notifiers {
		not, err := notifier.NewNotifier(n)
		if err != nil {
			panic(err)
		}
		notifiers = append(notifiers, not)
	}

	// Start watching the channels for new videos
	wg := watcher.Watch(ctx, func(task *taskman.Task) {
		rec, err := recorder.Record(ctx, task)
		if err != nil {
			tm.UpdateStep(task.Video.Id, taskman.StepErrored)
			return
		}
		lg.Debug("Recording finished", rec)

		for i, upl := range uploaders {
			lg.Info("Uploading", task.Video.Id, "to", cfg.Uploaders[i].Type)
			res, err := upl.Upload(ctx, rec)
			if err != nil {
				lg.Error("Error uploading", task.Video.Id, err)
				tm.UpdateStep(task.Video.Id, taskman.StepErrored)
				return
			}

			for _, not := range notifiers {
				lg.Debug("Notifying", task.Video.Id)
				not.NotifyUploaded(ctx, res)
			}
			tm.UpdateStep(task.Video.Id, taskman.StepDone)
		}
	})

	// Handle interrupt
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt)

	select {
	case <-c:
		time.Sleep(time.Second)
		lg.Info("Interrupted")
		cancel()
	case <-ctx.Done():
		time.Sleep(time.Second)
		lg.Debug("Context done")
	}

	lg.Info("Waiting for all goroutines to finish...")
	lg.Info("Press Ctrl+C again to force exit")
	go func() {
		<-c
		os.Exit(1)
	}()
	wg.Wait()
}
