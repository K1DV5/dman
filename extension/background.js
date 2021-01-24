
// download state constants, for easy recognition
S_DOWNLOADING = 0,
S_PAUSED = 1,
S_FAILED = 2,
S_WAIT_URL = 3,
S_REBUILDING = 4,
S_COMPLETED = 5
// native port
let native = chrome.runtime.connectNative('com.k1dv5.dman')

downloads = {
    // 1: {
    //     state: S_DOWNLOADING,  // downloading
    //     url: 'http://foo',
    //     dir: 'D:\\Downloads',
    //     filename: 'fooOcmdyfn/c+5/z9kgQqvVUlFFRVBVRBRVIf4nqqRTqdFkMjmo1WrfuIkPp6dqjFlEYIwGQRLtdltd19X3JyfDer1ejXmp+KAiABgRjBGMMRhjCKIAKBQKvN/5nJ19JV8s8Wxvj',
    //     size: '23.1MB',
    //     speed: '8MB/s',
    //     written: '15.32MB',
    //     percent: 35,
    //     conns: 13,
    //     eta: '5m23s',
    //     date: '12/16/2020',
    //     icon: 'hash',
    // },
}

// message sent to core, expecting response
downloadsPending = {}

settings = {
    conns: 1,
    categories: {},
    notify: {
        begin: true,
        end: true,
    }
}

// icons are stored in an object to prevent unnecessarily storing duplicate
// icons and waste storage. Also count the number of downloads using an icon to
// purge when not needed
icons = {
    // hash: {
    //     downloads: 1,
    //     url: 'data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAABAAAAAQCAYAAAAf8/9hAAAB4klEQVQ4jXWTwW4SURSGv8KECRZtQ4AY07DSxMFoSbd1U93UbX0SnoSFdGGXWHgJNdo3cMMsaxOjKKVAwcAMzj3HxcwwUNo/Ocmdyfn/c+5/z9kgQqvVUlFFRVBVRBRVIf4nqqRTqdFkMjmo1WrfuIkPp6dqjFlEYIwGQRLtdltd19X3JyfDer1ejXmp+KAiABgRjBGMMRhjCKIAKBQKvNzf37bt7JeYZy0EVKMDKHqjv/C72WyGpExme01ARKPUZaEER0dvUcBKp2kcH7MmoCoLgu/5dNwO/cEAFAr5PE6lgm3bYTGVhcDCA9GwpO/5nJ19JV8s8Wxvj8cvnkM2y8fPn/B9L/JL1wViEzudDk8ch/LODjqfM59OyT24z8NyGbfjrvq11gFw2e+zYVn87P7ix/k5w26Xf9MZm5s5ev1+5FdyhcQDUUARFa7HE1JL95z7PinLAg1z7uggNLFULHHV6zHzfGaBAcsiv7XF9WBAqVhA7zIxVq04DqP+JX+6vwkCg+fN+X5xwXhwxVOncreJIuErZGybw8M3PCoV8f6OmYyG5O5lOXj1eukZE4GVOYhh2zbVapXd6mLkV7Ccm0yiKopiWelbScsQua0DUd41GqhotM7RWqugomEBlWjNE4H/IEVCiAG6tNYAAAAASUVORK5CYII=',
    // },
}

// to change url
waitingUrl = undefined

// managing icons by keys
function addIconKey(hash, downCount) {
    if (hash in icons) {
        icons[hash].downloads += downCount
        if (icons[hash].downloads == 0) {
            delete icons[hash]
        }
    } else if (downCount > 0) {
        icons[hash] = {
            downloads: downCount,
        }
    }
}

chrome.storage.local.get(['downloads', 'settings', 'icons'], res => {
    if (res.downloads != undefined) {
        downloads = res.downloads
    }
    if (res.settings != undefined) {
        settings = res.settings
    }
    if (res.icons != undefined) {
        icons = res.icons
    }
})

// remove bottom bar when starting a new download
chrome.downloads.setShelfEnabled(false)

let DEFAULT_NOTIF_TIMEOUT = 5000
// show alerts
function notify(msg, id, timeout) {
    id = String(id)
    chrome.notifications.create(id || 'message', {
        title: 'Dman',
        message: msg,
        type: 'basic',
        iconUrl: chrome.extension.getURL('images/icon128.png')
    })
    if (timeout) {
        setTimeout(() => {
            chrome.notifications.clear(id)
        }, timeout)
    }
}

function addItem(browserId, url, dir, iconHash) {
    let id = Number(new Date().getTime().toString().slice(3, -2))
    downloadsPending[id] = {
        browserId,
        icon: iconHash,
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
        native.postMessage({
            id,
            type: 'new',
            url: info.url,
            filename: info.filename,  // filename will be used to know if resuming
            dir: info.dir
        })
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
        addIconKey(downloads[id].icon, -1)
        delete downloads[id]
        chrome.extension.getViews({type: 'popup'})[0]?.finishRemove([id])  // popup.finishRemove
        chrome.storage.local.set({downloads, icons})
    } else {
        native.postMessage({id, type: 'remove', dir: download.dir, filename: download.filename})
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
                chrome.downloads.search({id: downloadsPending[message.id].id}, items => {
                    addIconKey(downloadsPending[message.id].icon, -1)
                    delete downloadsPending[message.id]
                    chrome.downloads.resume(items[0].id)
                    notify(message.error + "\nContinuing in Downloads...", message.id, DEFAULT_NOTIF_TIMEOUT)
                })
            } else {
                notify(message.error, message.id, DEFAULT_NOTIF_TIMEOUT)
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
                popup.add(message.id)  // popup.addRow
                switchUpdates(true)
            }
            chrome.downloads.erase({id: downloadsPending[message.id].browserId}, () => {
                delete downloadsPending[message.id]
                if (settings.notify.begin) {
                    notify('Downloading ' + message.filename, message.id, DEFAULT_NOTIF_TIMEOUT)
                }
            })
        } else if (downloads[message.id].filename != message.filename) {  // resuming
            notify("Resume error: filenames don't match", message.id, DEFAULT_NOTIF_TIMEOUT)
        } else {
            downloads[message.id].state = S_DOWNLOADING
            if (popup) {
                popup.update(message.id)  // popup.addRow
                switchUpdates(true)
            }
        }
        updateBadge()
        chrome.storage.local.set({downloads, icons})
    },

    pause: message => {
        downloads[message.id].state = S_PAUSED
        chrome.extension.getViews({type: 'popup'})[0]?.update(message.id)  // popup.update
        chrome.storage.local.set({downloads})
        updateBadge()
        if (message.error != undefined) {
            notify(message.error, message.id, DEFAULT_NOTIF_TIMEOUT)
        }
    },

    failed: message => {
        downloads[message.id].state = S_FAILED
        notify('Downloading ' + downloads[message.id].filename + ' failed:\n' + message.error, message.id)  // keep message
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
        if (message.filename) {
            download.filename = message.filename
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
        if (settings.notify.end) {
            notify('Finished downloading ' + download.filename, message.id, DEFAULT_NOTIF_TIMEOUT)
        }
    },

    remove: message => {
        if (message.error) {
            notify(message.error, message.id, DEFAULT_NOTIF_TIMEOUT)
        }
        addIconKey(downloads[message.id].icon, -1)
        delete downloads[message.id]
        chrome.extension.getViews({type: 'popup'})[0]?.finishRemove([message.id])  // popup.finishRemove
        chrome.storage.local.set({downloads, icons})
    },

    error: message => {
        notify('Error: ' + message.error)
    },

    default: message => {
        notify('Unknown message type:' + message.type)
    }
}

native.onMessage.addListener(message => {
    (handlers[message.type] || handlers.default)(message)
})

// native.onDisconnect.addListener(() => {
//     chrome.storage.local.set({downloads})
// });

let pathSep = navigator.platform == 'Win32' ? '\\' : '/'

// for icon image urls. This is to be used to generate a key for the icons
// storage because the icon urls are data: urls thus long
function hash32(str) {
    let hash = 0, i
    for (i = 0; i < str.length; i++) {
        hash = (hash * 31 + str.charCodeAt(i)) | 0
    }
    return hash
}

chrome.downloads.onChanged.addListener(item => {
    if (item.filename == undefined) {
        return
    }
    chrome.downloads.pause(item.id, () => {
        if (waitingUrl) {
            chrome.downloads.search({id: item.id}, items => {
                item = items[0]
                let down = downloads[waitingUrl]
                down.url = item.finalUrl
                changeState(waitingUrl, S_DOWNLOADING)
                waitingUrl = undefined
                chrome.downloads.erase({id: waitingUrl})
            })
            return
        }
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
                // store icon in the icons registry
                let iconHash = hash32(iconUrl)
                addIconKey(iconHash, 1)
                icons[iconHash].url = iconUrl
                addItem(item.id, item.finalUrl, dir, iconHash)
            })
        })
    })
})

