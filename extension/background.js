// constants, for easy recognition
S_DOWNLOADING = 0,
S_PAUSED = 1,
S_FAILED = 2,
S_REBUILDING = 3,
S_COMPLETED = 4
// native port
let native = chrome.runtime.connectNative('com.k1dv5.dman');

downloads = {
    // 1: {
    //     state: S_DOWNLOADING,  // downloading
    //     url: 'http://foo',
    //     filename: 'foo',
    //     size: '23.1MB',
    //     speed: '8MB/s',
    //     percent: 35,
    //     connections: 13,
    //     eta: '5m23s',
    //     date: '12/16/2020'
    // },
    // 2: {
    //     state: S_PAUSED,  // paused
    //     url: 'http://foo',
    //     filename: 'foo',
    //     percent: 70,
    //     size: '23.1MB',
    //     date: '12/16/2020'
    // },
    // 3: {
    //     state: S_FAILED,  // failed
    //     url: 'http://foo',
    //     filename: 'foo',
    //     percent: 70,
    //     size: '23.1MB',
    //     date: '12/16/2020'
    // },
    // 4: {
    //     state: S_REBUILDING,  // rebuilding
    //     url: 'http://foo',
    //     filename: 'foo',
    //     percent: 70,
    //     size: '23.1MB',
    //     date: '12/16/2020'
    // },
    // 5: {
    //     state: S_COMPLETED,  // completed
    //     url: 'http://foo',
    //     filename: 'foo',
    //     size: '23.1MB',
    //     date: '12/16/2020'
    // },
}

function addItem(url) {
    // send to native
    native.postMessage({
        type: 'new',
        id: Number((Date.now()).toString().slice(2, -2)),
        url
    })
}

function changeState(id, to) {
    let info = downloads[id]
    if (info.state == S_REBUILDING) return  // rebuilding
    if (info.state == S_DOWNLOADING) {  // downloading
        if (to != S_PAUSED) return
        // pause
        native.postMessage({type: 'stop', id})
    } else if (to == null) {  // delete
        native.postMessage({type: 'delete', filename: info.filename})
    } else {  // paused / failed
        if (to != S_DOWNLOADING) return
        // resume
        native.postMessage({type: 'resume', filename: info.filename})
    }
}

native.onMessage.addListener(message => {
    if (message.type == 'info') {
        let ids = []
        for (let [id, info] of message.downloads) {
            ids.push(id)
            downloads[id] = info
        }
        chrome.extension.getViews({type: 'popup'})[0]?.update(ids)  // popup.addRow
    } else {
        let id = download.id
        delete download.id
        let type = download.type
        delete download.type
        downloads[id] = download
        if (type == 'new') {
            download.date = new Date().toLocaleDateString()
            downloads[id] = download
            // ? because the popup may be closed now
            chrome.extension.getViews({type: 'popup'})[0]?.addRow(download, id)  // popup.addRow
        } else {
            chrome.extension.getViews({type: 'popup'})[0]?.update(ids)  // popup.addRow
        }
        chrome.storage.local.set({downloads})
    }
});

// native.onDisconnect.addListener(() => {
//     chrome.storage.local.set({downloads})
// });
