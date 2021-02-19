# dman
A really fast download manager that is tightly integrated with the browser

Dman is a download manager I built after having some inconveniences with IDM,
the fastest download manager that I know of. As such, what it does internally
is largely the same as IDM, but I have improved some things.

The main differences with IDM are:

- It does not have its own graphical UI. Instead, it has a browser extension
  that acts as the UI and communicates with it. The logic for this is that we
  use a download manager along with a browser most of the time. So instead of
  hopping back and forth between the browser and the download manager, why not
  attach the download manager to the browser?
- If one really needs to use it outside of the browser though, it has a simple
  command line interface.

## Usage

### As an end user

Currently, only Google Chrome on Windows is supported. It may be easy to port
the extension to Firefox and the native part to Linux and Mac, and I will
gladly accept any help in that.

To install, Download the zip package from the [Releases
page](https://github.com/K1DV5/dman/releases/latest), unpack it and follow the
instructions in INSTALLATION.txt

After that, any file you download will be downloaded through `dman`. You can
pin the extension icon to the toolbar of the browser. You can interact with it
by clicking the icon. The interface is designed to be easy to understand.

### In another Go project

The heart of `dman` is the download package, and it can be imported as:

```go
import (
    ...
    "github.com/K1DV5/dman/dman/download"
    ...
)

```

API documentation is in TODO. In the mean time, you can see how you can use it
from `dman/extension.go`

### In another application

`dman` understands the native messaging protocol of Chrome Extensions. That
means you can interact with it from any program using that protocol. Again,
until documentation becomes available, look at `extension/background.go` for an
example of how you can interact with it.

## Notes

Although most of the core work is done (or so I think), there are some
incomplete things:

- At the moment, I focused on writing the extension for Google Chrome. It's
  tested with this but it may work with other Chromium based browsers. Firefox
  support needs to be done.
- It does not catch media on sites, like audio or video like IDM does. This may
  or may not be added. One argument against adding this is giving the user the
  freedom to choose any extension that does it. If something arrives in
  Downloads, this one will wait there and take it from there.
