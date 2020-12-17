uiInfos = {
    1: {
        state: 0,  // downloading
        url: 'http://foo',
        filename: 'foo',
        size: '23.1MB',
        speed: '8MB/s',
        percent: 35,
        connections: 13,
        eta: '5m23s',
        date: '12/16/2020'
    },
    2: {
        state: 1,
        url: 'http://foo',
        filename: 'foo',
        percent: 70,
        size: '23.1MB',
        date: '12/16/2020'
    },
    3: {
        state: 2,  // completed
        url: 'http://foo',
        filename: 'foo',
        size: '23.1MB',
        date: '12/16/2020'
    }
}

function addItem(url) {
    // send to native, then
    let id = Number((Date.now()).toString().slice(2, -2))
    let info = {
        url,
        filename: 'foo',
        size: '23.1MB',
        speed: '8MB/s',
        percent: '35%',
        conns: 'x13',
        eta: '5m23s',
        date: '12/16/2020'
    }
    uiInfos[id] = info
    // ? because the popup may be closed now
    chrome.extension.getViews()[1]?.addRow(info, id)  // popup.addRow
}

// var port = chrome.runtime.connectNative('com.k1dv5.dman');

// port.onMessage.addListener(function(msg) {
//     console.log("Received", msg);
// });

// port.onDisconnect.addListener(function() {
//     console.log("Disconnected");
// });

// port.postMessage({ text: "Hello, my_application" });
