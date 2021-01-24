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

#Notes

Although most of the core work is done (or so I think), there are some
incomplete things:

- At the moment, I focused on writing the extension for Google Chrome. It's
  tested with this but it may work with other Chromium based browsers. Firefox
  support needs to be done.
- It does not catch media on sites, like audio or video like IDM does. This may
  or may not be added. One argument against adding this is giving the user the
  freedom to choose any extension that does it. If something arrives in
  Downloads, this one will wait there and take it from there.
