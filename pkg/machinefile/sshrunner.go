package internal

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"golang.org/x/crypto/ssh/terminal"
)

func getSSHAuth(sr *SSHRunner) []string {
	var sshArgs []string
	
	if sr.AskPassword {
		fmt.Printf("Enter SSH password for %s@%s: ", sr.SshUser, sr.SshHost)
		bytePassword, err := terminal.ReadPassword(int(syscall.Stdin))
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error reading password: %v\n", err)
			os.Exit(1)
		}
		fmt.Println()
		sr.SshPassword = string(bytePassword)
	}
	
	if sr.SshPassword != "" {
		if _, err := exec.LookPath("sshpass"); err != nil {
			fmt.Fprintf(os.Stderr, "Error: sshpass is not installed. Please install it to use password authentication.\n")
			os.Exit(1)
		}
		sshArgs = append(sshArgs, "sshpass", "-p", sr.SshPassword, "ssh")
	} else {
		sshArgs = append(sshArgs, "ssh")
	}
    	
	if sr.SshPort != "" {
		sshArgs = append(sshArgs, "-p", sr.SshPort)
	}

	if sr.SshKeyPath != "" {
		sshArgs = append(sshArgs, "-i", sr.SshKeyPath)
	}
	
	sshArgs = append(sshArgs, "-o", "StrictHostKeyChecking=no")
	
	return sshArgs
}

func (sr *SSHRunner) RunCommand(command string, userName string, envVars map[string]string) error {
	expandedCommand := expandVariables(command, envVars)
	sshCommand := expandedCommand
	
	if len(envVars) > 0 {
		envPrefix := ""
		for key, value := range envVars {
			envPrefix += fmt.Sprintf("%s=%s ", key, value)
		}
		sshCommand = envPrefix + ";" + sshCommand
	}
	
	if userName != "" {
		sshCommand = fmt.Sprintf("sudo -u %s bash -c '%s'", userName, strings.Replace(sshCommand, "'", "'\"'\"'", -1))
	}
	
	sshArgs := getSSHAuth(sr)
	sshArgs = append(sshArgs, fmt.Sprintf("%s@%s", sr.SshUser, sr.SshHost), sshCommand)
	
	cmd := exec.Command(sshArgs[0], sshArgs[1:]...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	
	fmt.Printf("Executing remote command: %s\n", sshCommand)
	err := cmd.Run()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error running remote command: %s, %v\n", command, err)
		return err
	}
	return nil
}

func (sr *SSHRunner) CopyFile(srcPattern, dest string, isAdd bool) error {
    srcPattern = filepath.Join(sr.BaseDir, srcPattern)
    srcPattern = filepath.Clean(srcPattern)
    dest = filepath.Clean(dest)
    
    matches, err := filepath.Glob(srcPattern)
    if err != nil {
        fmt.Fprintf(os.Stderr, "Error with glob pattern: %v\n", err)
        return err
    }
    
    if len(matches) == 0 {
        fmt.Fprintf(os.Stderr, "No matches found for pattern: %s\n", srcPattern)
        return fmt.Errorf("no matches found")
    }
    
    for _, src := range matches {
        srcInfo, err := os.Stat(src)
        if err != nil {
            fmt.Fprintf(os.Stderr, "Error stating source file: %v\n", err)
            return err
        }
        
        remoteTmpDir := fmt.Sprintf("/tmp/dockerfile-run-%d", time.Now().UnixNano())
        err = sr.RunCommand(fmt.Sprintf("mkdir -p %s", remoteTmpDir), "", nil)
        if err != nil {
            return err
        }
        
        // Fix for scp command construction
        var scpArgs []string
        
        if sr.SshPassword != "" {
            if _, err := exec.LookPath("sshpass"); err != nil {
                fmt.Fprintf(os.Stderr, "Error: sshpass is not installed. Please install it to use password authentication.\n")
                os.Exit(1)
            }
            scpArgs = append(scpArgs, "sshpass", "-p", sr.SshPassword, "scp")
        } else {
            scpArgs = append(scpArgs, "scp")
        }
        
        // Add common options
        scpArgs = append(scpArgs, "-o", "StrictHostKeyChecking=no")
        
        // Add key if specified
        if sr.SshKeyPath != "" {
            scpArgs = append(scpArgs, "-i", sr.SshKeyPath)
        }

        // Add port if not default (scp uses -P, different from ssh's -p)
        if sr.SshPort != "" {
            scpArgs = append(scpArgs, "-P", sr.SshPort)
        }

        // Add -p flag to preserve file attributes
        scpArgs = append(scpArgs, "-p", "-r")
        
        // Add source and destination
        scpArgs = append(scpArgs, src, fmt.Sprintf("%s@%s:%s/", sr.SshUser, sr.SshHost, remoteTmpDir))
        
        scpCmd := exec.Command(scpArgs[0], scpArgs[1:]...)
        scpCmd.Stdout = os.Stdout
        scpCmd.Stderr = os.Stderr
        
        if err := scpCmd.Run(); err != nil {
            fmt.Fprintf(os.Stderr, "Error copying file to remote host: %v\n", err)
            return err
        }
        
        srcBase := filepath.Base(src)
        remoteSrc := filepath.Join(remoteTmpDir, srcBase)
        
        var mvCommand string
        if srcInfo.IsDir() && isAdd {
            mvCommand = fmt.Sprintf("mkdir -p %s && cp -a %s/* %s/ && rm -rf %s", dest, remoteSrc, dest, remoteTmpDir)
        } else {
            mvCommand = fmt.Sprintf("mkdir -p $(dirname %s) && cp -a %s %s && rm -rf %s", dest, remoteSrc, dest, remoteTmpDir)
        }
        
        if err := sr.RunCommand(mvCommand, "", nil); err != nil {
            return err
        }
        
        if isAdd {
            fmt.Printf("Added contents of %s to %s on %s (preserving attributes)\n", src, dest, sr.SshHost)
        } else {
            fmt.Printf("Copied %s to %s on %s (preserving attributes)\n", src, dest, sr.SshHost)
        }
    }
    
    return nil
}
