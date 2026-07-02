/*
 * @Author: SpenserCai
 * @Date: 2023-01-30 17:53:47
 * @version:
 * @LastEditors: SpenserCai
 * @LastEditTime: 2023-02-05 10:39:04
 * @Description: file content
 */
package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/andygrunwald/vdf"
)

// 声明一个结构体，有GamePath和pfxPath两个字段
type SteamApp struct {
	GamePath string
	pfxPath  string
}

type WineDllOverrides struct {
	DllName string
	Mode    string
}

// 导入json包

// 定义一个数组存放steam默认路径.steam/steam和.local/share/Steam
var steamPath = []string{
	".steam/steam",
	".local/share/Steam",
}

// 通过环境变量获取home路径
var homePath = os.Getenv("HOME")

func pathExists(path string) bool {
	if _, err := os.Stat(path); err == nil {
		return true
	}
	return false
}

func appendUnique(paths []string, path string) []string {
	if path == "" {
		return paths
	}
	for _, existing := range paths {
		if existing == path {
			return paths
		}
	}
	return append(paths, path)
}

func getSteamLibraryPaths() ([]string, error) {
	libraryPaths := []string{}
	for _, localPath := range steamPath {
		steamRoot := filepath.Join(homePath, localPath)
		steamAppsPath := filepath.Join(steamRoot, "steamapps")
		libraryFoldersPath := filepath.Join(steamAppsPath, "libraryfolders.vdf")
		if !pathExists(libraryFoldersPath) {
			continue
		}

		libraryPaths = appendUnique(libraryPaths, steamRoot)

		f, err := os.Open(libraryFoldersPath)
		if err != nil {
			return libraryPaths, err
		}
		p := vdf.NewParser(f)
		v, err := p.Parse()
		f.Close()
		if err != nil {
			return libraryPaths, err
		}

		libraryFolders, ok := v["libraryfolders"].(map[string]interface{})
		if !ok {
			continue
		}
		for _, value := range libraryFolders {
			library, ok := value.(map[string]interface{})
			if !ok {
				continue
			}
			path, ok := library["path"].(string)
			if ok {
				libraryPaths = appendUnique(libraryPaths, path)
			}
		}
	}
	return libraryPaths, nil
}

func getInstallDirFromManifest(manifestPath string, fallback string) string {
	f, err := os.Open(manifestPath)
	if err != nil {
		return fallback
	}
	defer f.Close()

	p := vdf.NewParser(f)
	v, err := p.Parse()
	if err != nil {
		return fallback
	}
	appState, ok := v["AppState"].(map[string]interface{})
	if !ok {
		return fallback
	}
	installDir, ok := appState["installdir"].(string)
	if !ok || installDir == "" {
		return fallback
	}
	return installDir
}

// 读取指定appid和游戏名的steamapps路径
func GetSteamAppsPath(appid string, gameName string) (SteamApp, error) {
	// 定义一个结构体变量,默认值为nil
	steamApp := SteamApp{}

	libraryPaths, err := getSteamLibraryPaths()
	if err != nil {
		return steamApp, err
	}
	for _, libraryPath := range libraryPaths {
		steamAppsPath := filepath.Join(libraryPath, "steamapps")
		manifestPath := filepath.Join(steamAppsPath, "appmanifest_"+appid+".acf")
		if !pathExists(manifestPath) {
			continue
		}

		installDir := getInstallDirFromManifest(manifestPath, gameName)
		tmpGamePath := filepath.Join(steamAppsPath, "common", installDir)
		if !pathExists(tmpGamePath) {
			return steamApp, fmt.Errorf("found %s but game directory is missing: %s", manifestPath, tmpGamePath)
		}
		tmpPfxPath := filepath.Join(steamAppsPath, "compatdata", appid, "pfx")
		if !pathExists(tmpPfxPath) {
			return steamApp, fmt.Errorf("found %s but Proton prefix is missing: %s; run the game once and try again", manifestPath, tmpPfxPath)
		}
		steamApp.GamePath = tmpGamePath
		steamApp.pfxPath = tmpPfxPath
		return steamApp, nil
	}
	return steamApp, fmt.Errorf("Steam app %s (%s) was not found in any configured Steam library", appid, gameName)
}

func CopyFile(src string, dst string) error {
	// 读取源文件
	srcFile, err := os.Open(src)
	if err != nil {
		return err
	}
	defer srcFile.Close()
	// 创建目标文件
	dstFile, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer dstFile.Close()
	// 拷贝文件
	_, err = io.Copy(dstFile, srcFile)
	if err != nil {
		return err
	}
	return nil
}

func UnLockEaGameDlc(steamApp SteamApp) error {
	// version.dll劫持
	if steamApp.GamePath == "" || steamApp.pfxPath == "" {
		return errors.New("GamePath or pfxPath is empty")
	}
	eaDesktopBasePath := filepath.Join(steamApp.pfxPath, "drive_c/Program Files/Electronic Arts/EA Desktop")
	if !pathExists(eaDesktopBasePath) {
		return fmt.Errorf("EA Desktop directory not found: %s; run the game once and try again", eaDesktopBasePath)
	}
	eaDesktopDirs, err := findEaDesktopDirs(eaDesktopBasePath)
	if err != nil {
		return err
	}
	if len(eaDesktopDirs) == 0 {
		return fmt.Errorf("could not find EADesktop.exe under %s", eaDesktopBasePath)
	}
	srcDll := "./EaUnLockerTool/ea_desktop/version.dll"
	for _, eaDesktopDir := range eaDesktopDirs {
		dstDll := filepath.Join(eaDesktopDir, "version.dll")
		if err := CopyFile(srcDll, dstDll); err != nil {
			return err
		}
		fmt.Printf("From %s to %s \n", srcDll, dstDll)
	}
	// 创建配置文件
	// 递归创建steamApp.pfxPath+"/drive_c/users/steamuser/AppData/Roaming/anadius/EA DLC Unlocker v2目录
	eaUnlockConfig := filepath.Join(steamApp.pfxPath, "drive_c/users/steamuser/AppData/Roaming/anadius/EA DLC Unlocker v2")
	fmt.Printf("eaUnLockConfig: %s \n", eaUnlockConfig)
	if err := os.MkdirAll(eaUnlockConfig, 0755); err != nil {
		return err
	}
	// 复制配置文件
	// 用/分割steamApp.GamePath，取最后一个元素作为游戏名
	gameConfigName := "g_" + filepath.Base(steamApp.GamePath) + ".ini"
	gameConfigSrc := filepath.Join("./EaUnLockerTool", gameConfigName)
	gameConfigDst := filepath.Join(eaUnlockConfig, gameConfigName)
	mainConfigSrc := "./EaUnLockerTool/config.ini"
	mainConfigDst := filepath.Join(eaUnlockConfig, "config.ini")
	if err := CopyFile(gameConfigSrc, gameConfigDst); err != nil {
		return err
	}
	if err := CopyFile(mainConfigSrc, mainConfigDst); err != nil {
		return err
	}
	fmt.Printf("From %s to %s \n", gameConfigSrc, gameConfigDst)
	fmt.Printf("From %s to %s \n", mainConfigSrc, mainConfigDst)
	return nil

}

func findEaDesktopDirs(eaDesktopBasePath string) ([]string, error) {
	eaDesktopDirs := []string{}
	err := filepath.WalkDir(eaDesktopBasePath, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			rel, err := filepath.Rel(eaDesktopBasePath, path)
			if err == nil && rel != "." && strings.Count(rel, string(os.PathSeparator)) >= 3 {
				return filepath.SkipDir
			}
			return nil
		}
		if d.Name() == "EADesktop.exe" {
			eaDesktopDir := filepath.Dir(path)
			if filepath.Base(eaDesktopDir) == "EA Desktop" {
				eaDesktopDirs = appendUnique(eaDesktopDirs, eaDesktopDir)
			}
		}
		return nil
	})
	sort.Strings(eaDesktopDirs)
	return eaDesktopDirs, err
}

func GetGeProtonPath(steamApp SteamApp) (string, error) {
	steamdataPath := strings.Split(steamApp.pfxPath, "/pfx")[0]
	// 读取steamdataPath+"/config_info"文件
	configInfo, err := os.ReadFile(steamdataPath + "/config_info")
	if err != nil {
		return "", err
	}
	// 第一行是版本号，第二行是proton路径
	protonVersion := strings.Split(string(configInfo), "\n")[0]
	protonPath := strings.Split(string(configInfo), "\n")[1]
	// 去除第二行protonVersion后面的部分
	protonPath = strings.Split(protonPath, protonVersion)[0] + protonVersion + "/proton"
	return protonPath, nil

}

func UpdataWineCfg(steamApp SteamApp) error {
	// 裁减steamApp.GamePath字符串/steamapps以及后面的内容
	steamInstallPath := strings.Split(steamApp.GamePath, "/steamapps")[0]
	// 去掉steamApp.pfxPath字符串的/pfx
	steamdataPath := strings.Split(steamApp.pfxPath, "/pfx")[0]
	// protonPath
	protonPath, err := GetGeProtonPath(steamApp)
	if err != nil {
		return err
	}
	commandString := fmt.Sprintf("STEAM_COMPAT_CLIENT_INSTALL_PATH=\"%s\" STEAM_COMPAT_DATA_PATH=\"%s\" WINEPREFIX=\"%s\" \"%s\" run %s/drive_c/windows/system32/winecfg.exe", steamInstallPath, steamdataPath, steamApp.pfxPath, protonPath, steamApp.pfxPath)
	fmt.Println(commandString)
	// 异步执行命令
	cmd := exec.Command("sh", "-c", commandString)
	cmd.Start()
	return nil

}

func UpdataDllOverrides(steamApp SteamApp) error {
	// TODO:通过修改该[Software\\Wine\\DllOverrides]下的"version"="native,builtin"来实现
	// 读取steamApp.pfxPath+"/user.reg"文件
	userRegPath := filepath.Join(steamApp.pfxPath, "user.reg")
	userReg, err := os.ReadFile(userRegPath)
	if err != nil {
		return err
	}
	versionSet := "\"version\"=\"native,builtin\""
	dllOverridesFlag := "[Software\\\\Wine\\\\DllOverrides]"
	if strings.Contains(string(userReg), versionSet) {
		return nil
	}

	userRegStringArray := strings.Split(string(userReg), "\n")
	dllOverridesLine := -1
	for i, line := range userRegStringArray {
		if strings.Contains(line, dllOverridesFlag) {
			dllOverridesLine = i
			break
		}
	}

	if dllOverridesLine == -1 {
		userRegStringArray = append(userRegStringArray, "", dllOverridesFlag, versionSet)
	} else {
		insertLine := len(userRegStringArray)
		for i := dllOverridesLine + 1; i < len(userRegStringArray); i++ {
			if strings.HasPrefix(userRegStringArray[i], "[") {
				insertLine = i
				break
			}
		}
		userRegStringArray = append(userRegStringArray[:insertLine], append([]string{versionSet}, userRegStringArray[insertLine:]...)...)
	}

	userRegString := strings.Join(userRegStringArray, "\n")
	if err := os.WriteFile(userRegPath, []byte(userRegString), 0644); err != nil {
		return err
	}
	return nil

}

func main() {
	// 将steamPath数组中的路径和homePath拼接
	for i := 0; i < len(steamPath); i++ {
		// 输出
		fmt.Println(homePath + "/" + steamPath[i])
	}
	steamApp, err := GetSteamAppsPath("1222670", "The Sims 4")
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
	fmt.Printf("GamePath: %s\n", steamApp.GamePath)
	fmt.Printf("pfxPath: %s\n", steamApp.pfxPath)
	unLockErr := UnLockEaGameDlc(steamApp)
	if unLockErr != nil {
		fmt.Println(unLockErr)
		os.Exit(1)
	}
	// UpdataWineCfg(steamApp)
	updataDllErr := UpdataDllOverrides(steamApp)
	if updataDllErr != nil {
		fmt.Println(updataDllErr)
		os.Exit(1)
	}
}
