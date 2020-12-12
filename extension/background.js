var port = chrome.runtime.connectNative('com.k1dv5.dman');

port.onMessage.addListener(function(msg) {
    console.log("Received", msg);
});

port.onDisconnect.addListener(function() {
    console.log("Disconnected");
});

port.postMessage({ text: "Hello, my_application" });
