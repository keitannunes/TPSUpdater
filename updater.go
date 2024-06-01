package main

import (
	"archive/zip"
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"github.com/BurntSushi/toml"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

var updaterVersion string = "0.1.1"

type Config struct {
	Updater UpdaterConfig `toml:"updater"`
}

type UpdaterConfig struct {
	Release  string `toml:"release"`
	Version  string `toml:"version"`
	Password string `toml:"password"`
}
type ApiResponse struct {
	Version       string `json:"version"`
	Uri           string `json:"uri"`
	Password      string `json:"password"`
	Name          string `json:"name"`
	DeleteCabinet bool   `json:"deleteCabinet"`
}

func extractZip(zipFile string, destDir string) error {
	// Open the ZIP file
	r, err := zip.OpenReader(zipFile)
	if err != nil {
		return err
	}
	defer r.Close()

	// Iterate through each file/directory
	for _, f := range r.File {
		// Calculate the destination path
		fpath := filepath.Join(destDir, f.Name)

		// Check for ZipSlip (Directory traversal)
		if !strings.HasPrefix(fpath, filepath.Clean(destDir)+string(os.PathSeparator)) {
			return fmt.Errorf("illegal file path: %s", fpath)
		}

		// Create directory tree
		if f.FileInfo().IsDir() {
			os.MkdirAll(fpath, os.ModePerm)
			continue
		}

		// Create all directories
		if err := os.MkdirAll(filepath.Dir(fpath), os.ModePerm); err != nil {
			return err
		}

		// Extract file
		outFile, err := os.OpenFile(fpath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, f.Mode())
		if err != nil {
			return err
		}

		rc, err := f.Open()
		if err != nil {
			return err
		}

		_, err = io.Copy(outFile, rc)

		// Close the file without defer to handle errors
		outFile.Close()
		rc.Close()

		if err != nil {
			return err
		}
	}

	return nil
}
func deleteDirectories(basePath, prefix string) error {
	// Read all files and directories from basePath
	files, err := os.ReadDir(basePath)
	if err != nil {
		return err
	}

	for _, file := range files {
		// Check if the file is a directory and starts with the specified prefix
		if file.IsDir() && strings.HasPrefix(file.Name(), prefix) {
			dirPath := filepath.Join(basePath, file.Name())
			fmt.Println("Deleting directory:", dirPath)
			// Delete the directory and its contents
			if err := os.RemoveAll(dirPath); err != nil {
				return err
			}
		}
	}

	return nil
}

func main() {
	fmt.Println("Starting Updater...")
	fmt.Println("Loading Config...")

	file, err := os.ReadFile("egts.toml")
	if err != nil {
		fmt.Println("Error opening Config file:", err)
		return
	}

	// Decode the TOML file into the struct
	var config Config
	if _, err := toml.Decode(string(file), &config); err != nil {
		fmt.Println("Error decoding Config:", err)
		return
	}
	fmt.Println("Getting Updater URI...")
	resp, err := http.Get("https://raw.githubusercontent.com/keitannunes/TPSDir/main/updaterUri.txt")
	if err != nil {
		// Handle error
		fmt.Println("Error making the request:", err)
		return
	}
	defer resp.Body.Close()

	// Read the response body
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		// Handle error
		fmt.Println("Error reading the response body:", err)
		return
	}
	updaterUri := strings.TrimSpace(string(body))
	resp, err = http.Get(updaterUri + "version")
	if err != nil {
		fmt.Println("Error making the request:", err)
		return
	}
	if resp.StatusCode != 200 {
		fmt.Println("Error getting the version:", resp.Status)
		return
	}
	body, err = io.ReadAll(resp.Body)
	if err != nil {
		// Handle error
		fmt.Println("Error reading the response body:", err)
		return
	}
	if string(body) != updaterVersion {
		fmt.Printf("Updater is outdated, please download the latest updater (curr: %s, latest: %s)\n", updaterVersion, string(body))
		return
	}
	fmt.Println("Checking latest version..")
	resp, err = http.Post(updaterUri+"releases/"+config.Updater.Release, "application/json", bytes.NewReader([]byte(`{"version": "`+config.Updater.Version+`", "password": "`+config.Updater.Password+`"}`)))
	if err != nil {
		fmt.Println("Error making the request:", err)
		return
	}
	if resp.StatusCode == 304 {
		fmt.Println("Game already up to date, Updater finished")
		return
	}
	if resp.StatusCode != 200 {
		fmt.Println("Error getting the version:", resp.Status)
		return
	}

	body, err = io.ReadAll(resp.Body)
	if err != nil {
		fmt.Println("Error reading the response body:", err)
		return
	}
	var apiResp ApiResponse
	if err := json.Unmarshal(body, &apiResp); err != nil {
		fmt.Println("Error decoding the response body:", err)
		return
	}
	fmt.Printf("TPS Client outdated, downloading update %s...\n", apiResp.Name)
	fmt.Printf("Download URI: %s\n", apiResp.Uri)
	fmt.Println("Press 'Enter' to continue...")
	bufio.NewReader(os.Stdin).ReadBytes('\n')
	resp, err = http.Get(apiResp.Uri)
	if err != nil {
		fmt.Println("Error making the request:", err)
		return
	}
	if resp.StatusCode != 200 {
		fmt.Println("Error downloading update ", resp.Status)
		return
	}
	body, err = io.ReadAll(resp.Body)
	if err != nil {
		fmt.Println("Error reading the response body:", err)
		return
	}
	err = os.WriteFile("update.zip", body, 0644) // Write the body to the file with read/write permissions
	if err != nil {
		fmt.Println("Error writing the response to a file:", err)
		return
	}
	fmt.Println("Extracting update...")
	err = extractZip("update.zip", ".")
	if err != nil {
		fmt.Println("Error extracting the update:", err)
		return
	}
	_ = os.Remove("update.zip")
	fmt.Println("Update finished...")
	if apiResp.DeleteCabinet {
		fmt.Println("Deleting cabinet...")
		err = deleteDirectories(".", "CabinetInfo")
		if err != nil {
			fmt.Println("Error deleting CabinetInfo (please manually delete):", err)
			return
		}
		fmt.Println("CabinetInfo deleted...")

	}
	fmt.Println("Updating config to reflect new version...")
	f, err := os.Create("tps.toml")
	if err != nil {
		fmt.Println("Error creating the config:", err)
	}
	defer f.Close()

	// Write the struct to the file as TOML
	if err := toml.NewEncoder(f).Encode(config); err != nil {
		fmt.Println("Error encoding the config:", err)
	}
	fmt.Println("Updater exiting...")
}
