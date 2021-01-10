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
    //     written: '15.32MB',
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
    //     date: '12/16/2020',
    //     error: "Foo bar"
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

// remove bottom bar when starting a new download
chrome.downloads.setShelfEnabled(false)

// downloads folder, set in setupDownListener() below
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
        native.postMessage({id, type: 'pause', id})
    } else if (to == null) {  // delete
        native.postMessage({id, type: 'delete', filename: info.filename, dir: downloadsPath})
    } else {  // paused / failed
        if (to != S_DOWNLOADING) return
        // resume
        native.postMessage({id, type: 'new', filename: info.filename, dir: downloadsPath})
    }
}

function pauseAll() {
    native.postMessage({type: 'pause-all'})
}

function switchUpdates(to) {
    native.postMessage({type: 'info', info: to})
}

// states indicating in progress
let progStates = [S_DOWNLOADING, S_REBUILDING]

function removeItem(id) {
    let download = downloads[id]
    if (download == undefined || progStates.includes(download.state)) {  // in progress, cannot remove
        return false
    }
    if (download.state == S_COMPLETED) {
        delete downloads[id]
    } else {
        native.postMessage({id, type: 'remove', dir: downloadsPath, filename: download.filename})
    }
    return true
}

function updateBadge() {
    let downs = Object.values(downloads).filter(d => progStates.includes(d.state)).length
    chrome.browserAction.setBadgeText({text: String(downs || '')})
}
updateBadge()

let handlers = {
    info: message => {
        let ids = []
        for (let stat of message.stats || []) {
            ids.push(stat.id)
            let download = downloads[stat.id]
            download.percent = stat.percent
            if (stat.rebuilding) {
                download.state = S_REBUILDING
                continue
            }
            download.written = stat.written
            download.conns = stat.conns || 0
            download.eta = stat.eta
            download.speed = stat.speed
        }
        let popup = chrome.extension.getViews({type: 'popup'})[0]
        if (popup) {
            popup.update(ids)
        } else {
            switchUpdates(false)
        }
    },

    new: message => {
        if (message.error) {
            return alert(message.error)
        }
        let popup = chrome.extension.getViews({type: 'popup'})[0]
        if (downloads[message.id] == undefined) {  // new download
            let download = {
                state: S_DOWNLOADING,
                url: message.url,
                filename: message.filename,
                size: message.size,
                percent: 0,
                written: '...',
                conns: 0,
                speed: '...',
                eta: '...',
                date: new Date().toLocaleDateString(),
            }
            downloads[message.id] = download
            if (popup) {
                popup.addRow(download, message.id)  // popup.addRow
                switchUpdates(true)
            }
        } else {  // resuming
            downloads[message.id].state = S_DOWNLOADING
            if (popup) {
                popup.update([message.id])  // popup.addRow
                switchUpdates(true)
            }
        }
        chrome.storage.local.set({downloads})
    },

    pause: message => {
        downloads[message.id].state = S_PAUSED
        chrome.extension.getViews({type: 'popup'})[0]?.update([message.id])  // popup.update
        chrome.storage.local.set({downloads})
    },

    failed: message => {
        downloads[message.id].state = S_FAILED
        downloads[message.id].error = message.error
        chrome.extension.getViews({type: 'popup'})[0]?.update([message.id])  // popup.update
        chrome.storage.local.set({downloads})
    },

    'pause-all': message => {
        let ids = []
        for (let stat of message.stats) {
            downloads[stat.id].state = S_PAUSED
            ids.push(stat.id)
        }
        chrome.extension.getViews({type: 'popup'})[0]?.update(ids)  // popup.update
        chrome.storage.local.set({downloads})
    },

    completed: message => {
        let download = downloads[message.id]
        download.state = S_COMPLETED
        if (!download.length && message.length) {
            download.length = message.length
        }
        updateBadge()
        chrome.extension.getViews({type: 'popup'})[0]?.update([message.id])  // popup.update
        for (let stat of ['percent', 'written', 'speed', 'eta', 'conns']) {
            delete download[stat]
        }
        if (Object.values(downloads).filter(d => progStates.includes(d.state)).length == 0) {
            switchUpdates(false)
        }
        chrome.storage.local.set({downloads})
    },

    remove: message => {
        if (message.error) {
            alert(message.error)
        }
        delete downloads[message.id]
        chrome.extension.getViews({type: 'popup'})[0]?.finishRemove([message.id])  // popup.finishRemove
    },

    error: message => {
        console.error('DMan error: ', message.error)
    },

    default: message => {
        alert('Unknown message type:' + message.type)
    }
}

native.onMessage.addListener(message => {
    (handlers[message.type] || handlers.default)(message)
    if (message.type != 'info') {
        updateBadge()
    }
})

// native.onDisconnect.addListener(() => {
//     chrome.storage.local.set({downloads})
// });

function setupDownListener(pingFilename) {
    let pathSep = navigator.platform == 'Win32' ? '\\' : '/'
    downloadsPath = pingFilename.slice(0, pingFilename.lastIndexOf(pathSep))
    chrome.downloads.onCreated.addListener(item => {
        chrome.downloads.pause(item.id, () => {
            addItem(item.finalUrl)
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
            setupDownListener(item.filename.current)
        })
    }
}

chrome.downloads.onChanged.addListener(getFilename)
chrome.downloads.download({url: 'data:,'})

