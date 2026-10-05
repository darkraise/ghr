package toolchain

import "context"

type node struct{ manifestTool }

func newNode(e *Env) Installer {
	return node{manifestTool{e: e, tool: "node", dir: "node", label: "Node", url: e.Sources.NodeManifest, fileOK: linuxX64}}
}

func (n node) Resolve(ctx context.Context, spec string) (Release, error) {
	v, f, err := n.pick(ctx, spec)
	if err != nil {
		return Release{}, err
	}
	return Release{Tool: "node", Version: v, Folder: v, URL: f.DownloadURL}, nil
}

func (n node) Install(ctx context.Context, rel Release, progress func(string)) error {
	return n.e.installArchive(ctx, n.dir, rel, progress)
}
