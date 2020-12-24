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
    //     conns: 13,
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

// downloads folder, set in setupListener() below
let downloadsPath

function addItem(url) {
    // send to native
    native.postMessage({
        type: 'new',
        id: Number((Date.now()).toString().slice(2, -2)),
        url,
        conns: 32,
        dir: downloadsPath
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

function switchUpdates(to) {
    native.postMessage({type: 'info', info: to})
}

native.onMessage.addListener(message => {
    if (message.type == 'info') {
        console.log("info")
        let ids = []
        for (let stat of message.stats || []) {
            ids.push(stat.id)
            let download = downloads[stat.id]
            download.percent = stat.percent
            download.conns = stat.conns
            download.eta = stat.eta
            download.speed = stat.speed
        }
        chrome.extension.getViews({type: 'popup'})[0]?.update(ids)  // popup.addRow
    } else if (message.type == "new") {
        let download = {
            state: S_DOWNLOADING,
            url: message.url,
            filename: message.filename,
            size: message.size,
            percent: 0,
            conns: 0,
            speed: '...',
            eta: '...',
            date: new Date().toLocaleDateString(),
        }
        downloads[message.id] = download
        // ? because the popup may be closed now
        let popup = chrome.extension.getViews({type: 'popup'})[0]
        if (popup) {
            popup.addRow(download, message.id)  // popup.addRow
            switchUpdates(true)
        }
        chrome.storage.local.set({downloads})
    } else if (message.type == "pause") {
        downloads[message.id].state = S_PAUSED
        chrome.extension.getViews({type: 'popup'})[0]?.update([message.id])  // popup.update
        chrome.storage.local.set({downloads})
    } else if (message.type == "resume") {
        downloads[message.id].state = S_DOWNLOADING
        chrome.extension.getViews({type: 'popup'})[0]?.update([message.id])  // popup.update
        chrome.storage.local.set({downloads})
    } else if (message.type == "completed") {
        downloads[message.id].state = S_COMPLETED
        chrome.extension.getViews({type: 'popup'})[0]?.update([message.id])  // popup.update
        let download = downloads[message.id]
        delete download.percent
        delete download.conns
        delete download.speed
        delete download.eta
        let progStates = [S_DOWNLOADING, S_REBUILDING]
        if (Object.values(downloads).filter(d => progStates.includes(d.state)).length == 0) {
            switchUpdates(false)
        }
        chrome.storage.local.set({downloads})
    } else if (message.type == "error") {
        console.error('DMan error: ', message.error)
    } else {
        alert('Unknown message type:' + message.type)
    }
});

// native.onDisconnect.addListener(() => {
//     chrome.storage.local.set({downloads})
// });

chrome.downloads.setShelfEnabled(false)

function setupListener(pingFilename) {
    let pathSep = navigator.platform == 'Win32' ? '\\' : '/'
    downloadsPath = pingFilename.slice(0, pingFilename.lastIndexOf(pathSep))
    chrome.downloads.onCreated.addListener(item => {
        chrome.downloads.pause(item.id, () => {
            addItem(item.url)
            chrome.downloads.erase({id: item.id})
        })
    })
}

// get downloads folder
function getFilename(item) {
    if (item.filename) {
        chrome.downloads.pause(item.id, () => {
            chrome.downloads.erase({id: item.id})
            chrome.downloads.onChanged.removeListener(getFilename)
            setupListener(item.filename.current)
        })
    }
}

chrome.downloads.onChanged.addListener(getFilename)
chrome.downloads.download({url: 'data:,'})

