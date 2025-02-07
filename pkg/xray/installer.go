package xray

import (
	"context"
	"fmt"
	"io"
	"time"

	. "github.com/leptonai/xray-manager/pkg/logger"
	"github.com/leptonai/xray-manager/pkg/ssh"
)

type Installer struct {
	ssh       *ssh.SSH
	logWriter io.Writer
}

func NewInstaller(ssh *ssh.SSH, logWriter io.Writer) *Installer {
	return &Installer{
		ssh:       ssh,
		logWriter: logWriter,
	}
}

func (installer *Installer) UpdateXRayClient(ctx context.Context, arch string, domain string) error {
	Logger.Infow("updating xray")

	cmds := []string{
		fmt.Sprintf("rm -rf /tmp/xray-linux-%s || true", arch),
		fmt.Sprintf(`DOWNLOAD_URL="https://%s/xray-linux-%s.tar.gz"`, domain, arch),
		`wget --quiet --retry-connrefused --waitretry=1 --read-timeout=20 --timeout=15 --tries=70 --directory-prefix=/tmp/ --continue ${DOWNLOAD_URL} -O - | tar -xzv -C /tmp/`,
		`if [ $? -ne 0 ]; then echo "failed to download xray"; exit 1; fi;`,
		fmt.Sprintf(`cd /tmp/xray-linux-%s && ./install.bash --remove`, arch),
		fmt.Sprintf(`cd /tmp/xray-linux-%s && ./install.bash`, arch),
		fmt.Sprintf(`cp /tmp/xray-linux-%s/install.bash /usr/local/etc/xray/install.bash`, arch),
	}

	sc := ssh.CommandsToBashScript(cmds...)
	ctx, cancel := context.WithTimeout(ctx, 3*time.Minute)
	defer cancel()
	b, err := installer.ssh.RunScript(ctx, sc, ssh.WithSudo(true))
	if err != nil {
		return err
	}
	fmt.Fprintf(installer.logWriter, "\noutput:\n\n%s\n\n", string(b))
	Logger.Infow("successfully updated xray")
	return nil
}
