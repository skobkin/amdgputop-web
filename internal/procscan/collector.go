package procscan

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"os"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
)

type rawProcess struct {
	pid         int
	uid         int
	user        string
	name        string
	command     string
	renderNode  string
	vramBytes   uint64
	gttBytes    uint64
	hasMemory   bool
	engineTotal uint64
	hasEngine   bool
}

type gpuCollection struct {
	processes []rawProcess
	hasMemory bool
	hasEngine bool
}

type collector struct {
	procRoot  *os.Root
	maxPIDs   int
	maxFDs    int
	lookup    *gpuLookup
	logger    *slog.Logger
	userCache map[int]string
}

type clientMemory struct {
	VRAM uint64
	GTT  uint64
}

func newCollector(procRoot string, maxPIDs, maxFDs int, lookup *gpuLookup, logger *slog.Logger) (*collector, error) {
	if logger == nil {
		logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	root, err := os.OpenRoot(procRoot)
	if err != nil {
		return nil, fmt.Errorf("open proc root: %w", err)
	}

	return &collector{
		procRoot:  root,
		maxPIDs:   maxPIDs,
		maxFDs:    maxFDs,
		lookup:    lookup,
		logger:    logger,
		userCache: make(map[int]string),
	}, nil
}

func (c *collector) collect() (map[string]gpuCollection, error) {
	entries, err := fs.ReadDir(c.procRoot.FS(), ".")
	if err != nil {
		return nil, err
	}

	results := make(map[string]gpuCollection)
	var scanned int

	for _, entry := range entries {
		if c.maxPIDs > 0 && scanned >= c.maxPIDs {
			break
		}
		if !entry.IsDir() {
			continue
		}
		pid, err := strconv.Atoi(entry.Name())
		if err != nil || pid <= 0 {
			continue
		}

		procDir, err := c.procRoot.OpenRoot(entry.Name())
		if err != nil {
			continue
		}

		procs := c.scanProcess(pid, procDir)
		if err := procDir.Close(); err != nil {
			c.logger.Debug("failed to close proc dir", "pid", pid, "err", err)
		}

		if len(procs) == 0 {
			continue
		}

		for gpuID, procList := range procs {
			col := results[gpuID]
			col.processes = append(col.processes, procList...)
			for _, raw := range procList {
				if raw.hasMemory {
					col.hasMemory = true
				}
				if raw.hasEngine {
					col.hasEngine = true
				}
			}
			results[gpuID] = col
		}

		scanned++
	}

	return results, nil
}

func (c *collector) scanProcess(pid int, procDir *os.Root) map[string][]rawProcess {
	comm, err := readTrimmed(procDir, "comm")
	if err != nil {
		return nil
	}

	cmdline, err := procDir.ReadFile("cmdline")
	if err != nil {
		cmdline = nil
	}
	command := formatCmdline(cmdline)

	uid, err := readUID(procDir, "status")
	if err != nil {
		return nil
	}

	userName := c.lookupUser(uid)

	fdEntries, err := fs.ReadDir(procDir.FS(), "fd")
	if err != nil {
		return nil
	}

	result := make(map[string]*rawProcess)
	clientTotals := make(map[string]map[int]clientMemory)
	fdCount := 0
	fdBasePath := filepath.Join(procDir.Name(), "fd")

	for _, fdEntry := range fdEntries {
		if c.maxFDs > 0 && fdCount >= c.maxFDs {
			break
		}
		fdCount++

		fdName := fdEntry.Name()
		fdPath := filepath.Join("fd", fdName)
		target, err := procDir.Readlink(fdPath)
		if err != nil {
			continue
		}
		if strings.HasSuffix(target, " (deleted)") {
			target = strings.TrimSuffix(target, " (deleted)")
		}
		if !filepath.IsAbs(target) {
			target = filepath.Clean(filepath.Join(fdBasePath, target))
		} else {
			target = filepath.Clean(target)
		}

		entry, ok := c.lookup.match(target)
		if !ok {
			continue
		}

		fdinfoPath := filepath.Join("fdinfo", fdName)
		data, err := procDir.ReadFile(fdinfoPath)
		if err != nil {
			continue
		}
		metrics := parseFDInfo(data)

		raw := result[entry.gpuID]
		if raw == nil {
			raw = &rawProcess{
				pid:        pid,
				uid:        uid,
				user:       userName,
				name:       comm,
				command:    command,
				renderNode: entry.base,
			}
			result[entry.gpuID] = raw
		}

		if metrics.HasMemory {
			if metrics.ClientID > 0 {
				if _, ok := clientTotals[entry.gpuID]; !ok {
					clientTotals[entry.gpuID] = make(map[int]clientMemory)
				}
				prev := clientTotals[entry.gpuID][metrics.ClientID]
				var deltaVRAM, deltaGTT uint64
				if metrics.VRAMBytes > prev.VRAM {
					deltaVRAM = metrics.VRAMBytes - prev.VRAM
					prev.VRAM = metrics.VRAMBytes
				}
				if metrics.GTTBytes > prev.GTT {
					deltaGTT = metrics.GTTBytes - prev.GTT
					prev.GTT = metrics.GTTBytes
				}
				clientTotals[entry.gpuID][metrics.ClientID] = prev
				if deltaVRAM > 0 {
					raw.vramBytes += deltaVRAM
					raw.hasMemory = true
				}
				if deltaGTT > 0 {
					raw.gttBytes += deltaGTT
					raw.hasMemory = true
				}
			} else {
				raw.vramBytes += metrics.VRAMBytes
				raw.gttBytes += metrics.GTTBytes
				raw.hasMemory = true
			}
		}
		if metrics.HasEngine {
			raw.engineTotal += metrics.EngineTotal
			raw.hasEngine = true
		}

	}

	if len(result) == 0 {
		return nil
	}

	out := make(map[string][]rawProcess, len(result))
	for gpuID, raw := range result {
		out[gpuID] = append(out[gpuID], *raw)
	}
	return out
}

func (c *collector) lookupUser(uid int) string {
	if name, ok := c.userCache[uid]; ok {
		return name
	}
	name := strconv.Itoa(uid)
	if u, err := user.LookupId(strconv.Itoa(uid)); err == nil {
		if u.Username != "" {
			name = u.Username
		}
	}
	c.userCache[uid] = name
	return name
}

type gpuEntry struct {
	gpuID string
	path  string
	base  string
}

type gpuLookup struct {
	byPath map[string]gpuEntry
	byBase map[string]gpuEntry
}

func newGPULookup(gpus []string, renderNodes map[string]string) *gpuLookup {
	byPath := make(map[string]gpuEntry)
	byBase := make(map[string]gpuEntry)

	for _, gpuID := range gpus {
		path := renderNodes[gpuID]
		if path == "" {
			continue
		}
		base := filepath.Base(path)
		entry := gpuEntry{
			gpuID: gpuID,
			path:  path,
			base:  base,
		}
		byPath[path] = entry
		byBase[base] = entry
	}

	return &gpuLookup{
		byPath: byPath,
		byBase: byBase,
	}
}

func (l *gpuLookup) match(target string) (gpuEntry, bool) {
	if entry, ok := l.byPath[target]; ok {
		return entry, true
	}
	if entry, ok := l.byBase[filepath.Base(target)]; ok {
		return entry, true
	}
	return gpuEntry{}, false
}

func readTrimmed(root *os.Root, name string) (string, error) {
	if root == nil {
		return "", fs.ErrNotExist
	}
	data, err := root.ReadFile(name)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(data)), nil
}

func readUID(root *os.Root, name string) (int, error) {
	if root == nil {
		return 0, fs.ErrNotExist
	}
	data, err := root.ReadFile(name)
	if err != nil {
		return 0, err
	}
	lines := strings.Split(string(data), "\n")
	for _, line := range lines {
		if !strings.HasPrefix(line, "Uid:") {
			continue
		}
		fields := strings.Fields(line[len("Uid:"):])
		if len(fields) == 0 {
			continue
		}
		uid, err := strconv.Atoi(fields[0])
		if err != nil {
			return 0, err
		}
		return uid, nil
	}
	return 0, errors.New("uid not found")
}

func formatCmdline(data []byte) string {
	if len(data) == 0 {
		return ""
	}
	parts := strings.Split(string(data), "\x00")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		if part != "" {
			out = append(out, part)
		}
	}
	cmd := strings.Join(out, " ")
	if len(cmd) > 256 {
		return cmd[:256]
	}
	return cmd
}
