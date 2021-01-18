
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
    //     dir: 'D:\\Downloads',
    //     filename: 'foo',
    //     size: '23.1MB',
    //     speed: '8MB/s',
    //     written: '15.32MB',
    //     percent: 35,
    //     conns: 13,
    //     eta: '5m23s',
    //     date: '12/16/2020'
    // },
}

// message sent to core, expecting response
downloadsPending = {}

settings = {
    conns: 1,
    categories: {}
}

chrome.storage.local.get(['downloads', 'settings'], res => {
    if (res.downloads != undefined) {
        downloads = res.downloads
    }
    if (res.settings != undefined) {
        settings = res.settings
    }
})

// remove bottom bar when starting a new download
chrome.downloads.setShelfEnabled(false)

function addItem(browserId, url, dir, icon) {
    let id = Number(new Date().getTime().toString().slice(3, -2))
    downloadsPending[id] = {
        browserId,
        icon
    }
    // send to native
    native.postMessage({
        type: 'new',
        id,
        url,
        dir,
        conns: settings.conns,
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
        native.postMessage({id, type: 'delete', filename: info.filename, dir: info.dir})
    } else {  // paused / failed
        if (to != S_DOWNLOADING) return
        // resume
        native.postMessage({id, type: 'new', filename: info.filename, dir: info.dir})
    }
}

function pauseAll() {
    native.postMessage({type: 'pause-all'})
}

function switchUpdates(to) {
    native.postMessage({type: 'info', info: to})
}

function openFile(id) {
    let down = downloads[id]
    native.postMessage({type: 'open', filename: down.filename, dir: down.dir})
}

function openDir(id) {
    native.postMessage({type: 'open', dir: downloads[id]?.dir})
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
        chrome.extension.getViews({type: 'popup'})[0]?.finishRemove([id])  // popup.finishRemove
        chrome.storage.local.set({downloads})
    } else {
        native.postMessage({id, type: 'remove', filename: download.filename})
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
        let popup = chrome.extension.getViews({type: 'popup'})[0]
        for (let stat of message.stats) {
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
            popup?.update(stat.id)
        }
        if (popup == undefined) {
            switchUpdates(false)
        }
    },

    new: message => {
        if (message.error) {
            if (downloadsPending[message.id] != undefined) {
                chrome.downloads.search({id: downloadsPending[message.id].id}, item => {
                    chrome.downloads.resume(item.id)
                    alert(message.error + "\nContinuing in Downloads...")
                })
            } else {
                alert(message.error)
            }
            return
        }
        let popup = chrome.extension.getViews({type: 'popup'})[0]
        if (downloads[message.id] == undefined) {  // new download
            let download = {
                state: S_DOWNLOADING,
                url: message.url,
                dir: message.dir,
                filename: message.filename,
                size: message.size,
                percent: 0,
                written: '...',
                conns: 0,
                speed: '...',
                eta: '...',
                date: Date.now(),
                icon: downloadsPending[message.id].icon,
            }
            downloads[message.id] = download
            if (popup) {
                popup.addRow(download, message.id)  // popup.addRow
                switchUpdates(true)
            }
            chrome.downloads.erase({id: downloadsPending[message.id].browserId}, () => {
                delete downloadsPending[message.id]
            })
        } else if (downloads[message.id].filename != message.filename) {  // resuming
            alert("Resume error: filenames don't match")
        } else {
            downloads[message.id].state = S_DOWNLOADING
            if (popup) {
                popup.update(message.id)  // popup.addRow
                switchUpdates(true)
            }
        }
        updateBadge()
        chrome.storage.local.set({downloads})
    },

    pause: message => {
        downloads[message.id].state = S_PAUSED
        chrome.extension.getViews({type: 'popup'})[0]?.update(message.id)  // popup.update
        chrome.storage.local.set({downloads})
        updateBadge()
        if (message.error != undefined) {
            alert(message.error)
        }
    },

    failed: message => {
        downloads[message.id].state = S_FAILED
        downloads[message.id].error = message.error
        chrome.extension.getViews({type: 'popup'})[0]?.update(message.id)  // popup.update
        chrome.storage.local.set({downloads})
        updateBadge()
    },

    'pause-all': message => {
        let popup = chrome.extension.getViews({type: 'popup'})[0]
        for (let stat of message.stats) {
            downloads[stat.id].state = S_PAUSED
            popup?.update(ids)  // popup.update
        }
        chrome.storage.local.set({downloads})
        updateBadge()
    },

    completed: message => {
        let download = downloads[message.id]
        download.state = S_COMPLETED
        if (!download.length && message.length) {
            download.length = message.length
        }
        updateBadge()
        chrome.extension.getViews({type: 'popup'})[0]?.update(message.id)  // popup.update
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
        chrome.storage.local.set({downloads})
    },

    error: message => {
        alert('Error:' + message.error)
    },

    default: message => {
        alert('Unknown message type:' + message.type)
    }
}

native.onMessage.addListener(message => {
    (handlers[message.type] || handlers.default)(message)
})

// native.onDisconnect.addListener(() => {
//     chrome.storage.local.set({downloads})
// });

let pathSep = navigator.platform == 'Win32' ? '\\' : '/'

chrome.downloads.onChanged.addListener(item => {
    if (item.filename == undefined) {
        return
    }
    chrome.downloads.pause(item.id, () => {
        // find the dir
        let fpath = item.filename.current
        let dirEnd = fpath.lastIndexOf(pathSep)
        let dir = fpath.slice(0, dirEnd)
        let fname = fpath.slice(dirEnd)
        let extStart = fname.lastIndexOf('.')
        if (extStart < 0) {
            dir = dir
        } else {
            let extension = fname.slice(extStart + 1)
            main: for (let [category, extensions] of Object.entries(settings.categories)) {
                for (let ext of extensions) {
                    if (ext == extension) {
                        dir += pathSep + category
                        break main
                    }
                }
            }
        }
        chrome.downloads.search({id: item.id}, items => {
            item = items[0]
            chrome.downloads.getFileIcon(item.id, {size: 16}, iconUrl => {
                addItem(item.id, item.finalUrl, dir, iconUrl)
            })
        })
    })
})

