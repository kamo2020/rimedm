package core

import (
	"bufio"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"

	"github.com/goccy/go-yaml"
)

var version = "1.0.5"

type Options struct {
	RestartRimeCmd string   `yaml:"restart_rime_cmd"`
	UserPath       string   `yaml:"user_path"`
	DictPaths      []string `yaml:"dict_paths"`
	SyncOnChange   bool     `yaml:"sync_on_change"`
}

func ParseOptions() (Options, string) {
	configDir, _ := os.UserConfigDir()
	configPath := filepath.Join(configDir, "rimedm", "config.yaml")

	var flags Options
	flag.Func("d", "(当使用配置文件时可选)主词典文件(方案名.dict.yaml)路径，通过主词典会自动加载其他拓展词典，无需指定拓展词典。\n支持多个主词典文件，e.g: rimedm -d ./xkjd6.dict.yaml -d ./xhup.dict.txt", func(path string) error {
		if flags.DictPaths == nil {
			flags.DictPaths = make([]string, 0)
		}
		flags.DictPaths = append(flags.DictPaths, path)
		return nil
	})

	flag.StringVar(&flags.UserPath, "u", "", "(可选)用户词典路径")
	flag.StringVar(&flags.RestartRimeCmd, "cmd", "", "(可选)同步到词典文件后，用于重新部署rime的命令，使更改即时生效，不同的系统环境下需要不同的命令")
	flag.BoolVar(&flags.SyncOnChange, "sync", true, "(可选)是否在每次添加、删除、修改时立即同步到词典文件，默认为 true")
	flag.StringVar(&configPath, "c", configPath, "(可选)配置文件路径，默认位置:"+configPath)
	showVersion := flag.Bool("v", false, "显示版本号")
	flag.Parse()
	if *showVersion {
		fmt.Println(version)
		os.Exit(0)
	}

	configPath = fixPath(configPath)
	opts := parseFromFile(configPath)

	if len(flags.DictPaths) > 0 {
		opts.DictPaths = flags.DictPaths
		opts.UserPath = ""
	}
	if flags.UserPath != "" {
		opts.UserPath = flags.UserPath
	}
	if flags.RestartRimeCmd != "" {
		opts.RestartRimeCmd = flags.RestartRimeCmd
	}
	if !flags.SyncOnChange {
		opts.SyncOnChange = false
	}

	if opts.DictPaths == nil || len(opts.DictPaths) == 0 {
		log.Fatalf("未指定词典文件，请检查配置文件[%s]或通过 -d 指定词典文件", configPath)
	}

	for i := 0; i < len(opts.DictPaths); i++ {
		opts.DictPaths[i] = fixPath(opts.DictPaths[i])
	}
	opts.UserPath = fixPath(opts.UserPath)
	return opts, configPath
}

func initConfigFile(filePath string) {
	dirPath := filepath.Dir(filePath)
	_, err := os.OpenFile(dirPath, os.O_RDONLY, 0666)
	if err != nil {
		if os.IsNotExist(err) {
			err = os.MkdirAll(dirPath, os.ModePerm)
			if err != nil {
				log.Fatalf("mkdir [%s] err : %s", dirPath, err)
			}
		} else {
			log.Fatalf("open [%s] err : %s", dirPath, err)
		}
	}
	file, err := os.Create(filePath)
	if err != nil {
		log.Fatalf("create [%s] err : %s", filePath, err)
	}
	defer func() {
		_ = file.Close()
	}()
	_, err = file.WriteString(initConfigTemplate())
	if err != nil {
		log.Fatalf("write [%s] err : %s", filePath, err)
	}
}

func initConfigTemplate() string {
	dictPath, restartRimeCmd := osRimeDefaultValue()
	return fmt.Sprintf(`# Rime Dict Manager config file
# This file is generated by rime-dict-manager.

# dict_paths 是主词典文件的路径，本程序会自动加载主词典所引用的其他拓展词典。
# 支持多个主词典，注意是主词典，请不要将主词典与其所属拓展词典一同写在dict_paths:下
# 在Linux + Fcitx5 + Fcitx5-Rime下，词典的路径一般是: $HOME/.local/share/fcitx5/rime/方案名.dict.yaml
# 在Windows + 小狼毫下，词典的路径一般是: %%Appdata%%/Rime/方案名.dict.yaml
#dict_paths:
#	- 主词典1文件路径
#	- 主词典2文件路径
# 禁止
#	- 主词典1下的拓展词典文件路径
dict_paths:
	- %s

# user_path 是用户词典路径，可以为空，
#	当指定了用户词典时，在添加新词时，用户词典会作为优先的添加选项。
#	如果没有指定用户词典，你也可以在添加时的选项中选择用户词典或其他词典。
#user_path: 

# 是否在每次添加、删除、修改时立即同步到词典文件，默认为 true
sync_on_change: true 
# 在同步词典文件时，通过这个命令来重启 rime, 不同的系统环境下需要不同的命令。
# 在Linux + Fcitx5 下可通过此命令来重启 rime: 
#	dbus-send --session --print-reply --dest=org.fcitx.Fcitx5 /controller org.fcitx.Fcitx.Controller1.SetConfig string:'fcitx://config/addon/rime' variant:string:''
# 在Windows + 小狼毫 下可通过此命令来重启 rime(注意程序版本): 
#	C:\PROGRA~2\Rime\weasel-0.14.3\WeaselDeployer.exe /deploy
#	注:PROGRA~2 = Program Files (x86) PROGRA~1 = Program Files
restart_rime_cmd: %s`, dictPath, restartRimeCmd)
}

func osRimeDefaultValue() (dictPath, restartRimeCmd string) {
	configDir, err := os.UserConfigDir()
	if err != nil {
		return "", ""
	}
	switch runtime.GOOS {
	case "windows":
		// find rime install path
		dirEntries, err := os.ReadDir("C:\\PROGRA~2\\Rime")
		var maxVersion string
		if err == nil && len(dirEntries) > 0 {
			for _, dir := range dirEntries {
				if dir.IsDir() && strings.HasPrefix(dir.Name(), "weasel") {
					dirName := dir.Name()
					if compareVersion(dirName, maxVersion) {
						maxVersion = dirName
					}
				} else {
					continue
				}
			}
		}
		if maxVersion != "" {
			restartRimeCmd = filepath.Join("C:\\PROGRA~2\\Rime", maxVersion, "WeaselDeployer.exe") + " /deploy"
		}
		defaultSchema := findRimeDefaultSchema(filepath.Join(configDir, "rime", "default.custom.yaml"))
		dictPath = filepath.Join(configDir, "Rime", defaultSchema+".dict.yaml")
	case "dwain":
		homeDir, err := os.UserHomeDir()
		if err != nil {
			return "", ""
		}
		restartRimeCmd = "\"/Library/Input Methods/Squirrel.app/Contents/MacOS/Squirrel\" --reload" // mabye
		defaultSchema := findRimeDefaultSchema(filepath.Join(homeDir, "Library", "Rime", "default.custom.yaml"))
		dictPath = filepath.Join(homeDir, "Library", "Rime", defaultSchema+".dict.yaml")
	default:
		restartRimeCmd = "dbus-send --session --print-reply --dest=org.fcitx.Fcitx5 /controller org.fcitx.Fcitx.Controller1.SetConfig string:'fcitx://config/addon/rime' variant:string:''"
		defaultSchema := findRimeDefaultSchema("$HOME/.local/share/fcitx5/rime/default.custom.yaml")
		dictPath = fmt.Sprintf("$HOME/.local/share/fcitx5/rime/%s.dict.yaml", defaultSchema)
	}
	return
}

func parseFromFile(path string) Options {
	path = fixPath(path)
	file, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			initConfigFile(path)
			file, err = os.Open(path)
			if err != nil {
				log.Fatalf("init config file [%s] err : %s", path, err)
			}
		} else {
			log.Fatalf("open [%s] err : %s", path, err)
		}
	}
	defer func() {
		_ = file.Close()
	}()
	stat, err := file.Stat()
	if err != nil {
		log.Fatalf("file stat [%s] err : %s", path, err)
	}
	bs := make([]byte, stat.Size())
	_, _ = file.Read(bs)
	var opts Options
	err = yaml.Unmarshal(bs, &opts)
	if err != nil {
		log.Fatalf("parse config [%s] err : %s", path, err)
	}
	return opts
}

func findRimeDefaultSchema(rimeConfigPath string) string {
	file, err := os.Open(fixPath(rimeConfigPath))
	if err != nil {
		return ""
	}
	defer func() {
		_ = file.Close()
	}()
	reader := bufio.NewReader(file)
	for {
		line, eof := reader.ReadString('\n')
		if eof != nil {
			break
		}
		if i := strings.Index(line, "- schema:"); i != -1 {
			return strings.TrimSpace(line[i+len("- schema:"):])
		}
	}
	return ""
}

func fixPath(path string) string {
	if strings.HasPrefix(path, "~") {
		homeDir, err := os.UserHomeDir()
		if err != nil {
			panic(err)
		}
		path = homeDir + path[1:]
	}
	return os.ExpandEnv(path)
}

func parseVersion(version string) []int {
	ret := make([]int, 0)
	reg := regexp.MustCompile(`\d+`)
	res := reg.FindAllString(version, -1)
	for _, v := range res {
		num, err := strconv.Atoi(v)
		if err != nil {
			fmt.Println("convert error", v)
		}
		ret = append(ret, num)
	}
	return ret
}

func compareVersion(v1, v2 string) bool {
	vi := parseVersion(v1)
	vj := parseVersion(v2)
	for k := 0; k < len(vi) && k < len(vj); k++ {
		if vi[k] != vj[k] {
			return vi[k] > vj[k]
		}
	}
	return v1 > v2
}
