
// download state constants, for easy recognition
states = {
    downloading: 0,
    paused: 1,
    failed: 2,
    urlPending: 3,
    rebuilding: 4,
    completed: 5,
}
// states indicating in progress
const progStates = [states.downloading, states.rebuilding]
// platform specific file path separator
const pathSep = navigator.platform == 'Win32' ? '\\' : '/'
// duration before clearing notifications, ns
const notifyTimeout = 5000
// remove bottom bar when starting a new download
chrome.downloads.setShelfEnabled(false)

// show alerts
function notify(title, contextMessage, id, timeout) {
    id = String(id)
    chrome.notifications.create(id || 'message', {
        title: 'Dman',
        message: title,
        type: 'basic',
        contextMessage,
        iconUrl: chrome.extension.getURL('images/icon128.png')
    })
    if (timeout) {
        setTimeout(() => {
            chrome.notifications.clear(id)
        }, timeout)
    }
}

// for icon image urls. This is to be used to generate a key for the icons
// storage because the icon urls are data: urls thus long
function hash32(str) {
    let hash = 0, i
    for (i = 0; i < str.length; i++) {
        hash = (hash * 31 + str.charCodeAt(i)) | 0
    }
    return hash
}

class Downloads {

    constructor() {
        // download items
        this.items = {
            // 1: {
            //     state: states.downloading,  // downloading
            //     url: 'http://foo',
            //     dir: 'D:\\Downloads',
            //     filename: 'fooOcmdyfn/c+5/z9kgQqvVUlFFRVBVRBRVIf4nqqRTqdFkMjmo1WrfuIkPp6dqjFlEYIwGQRLtdltd19X3JyfDer1ejXmp+KAiABgRjBGMMRhjCKIAKBQKvN/5n',
            //     size: '23.1MB',
            //     speed: '8MB/s',
            //     written: '15.32MB',
            //     percent: 35,
            //     conns: 13,
            //     eta: '5m23s',
            //     date: '12/16/2020',
            //     icon: 'hash',
            // }
        }

        // settings
        this.settingsDefault = {
            conns: 1,
            categories: {
                Compressed: ['zip', 'rar'],
                Documents: ['pdf', 'mobi', 'epub'],
                Music: ['mp3', 'm4a'],
                Programs: ['exe'],
                Video: ['mp4'],
            },
            notify: {
                begin: true,
                end: true,
            }
        }

        // icons are stored in an object to prevent unnecessarily storing duplicate
        // icons and waste storage. Also count the number of downloads using an icon to
        // purge when not needed
        this.icons = {
            // hash1: {
            //     downloads: 1,
            //     url: 'data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAABAAAAAQCAYAAAAf8/9hAAAB4klEQVQ4jXWTwW4SURSGv8KECRZtQ4AY07DSxMFoSbd1U93UbX0SnoSFdGGXWHgJNdo3cMMsaxOjKKVAwcAMzj3HxcwwUNo/Ocmdyfn/c+5/z9kgQqvVUlFFRVBVRBRVIf4nqqRTqdFkMjmo1WrfuIkPp6dqjFlEYIwGQRLtdltd19X3JyfDer1ejXmp+KAiABgRjBGMMRhjCKIAKBQKvNzf37bt7JeYZy0EVKMDKHqjv/C72WyGpExme01ARKPUZaEER0dvUcBKp2kcH7MmoCoLgu/5dNwO/cEAFAr5PE6lgm3bYTGVhcDCA9GwpO/5nJ19JV8s8Wxvj8cvnkM2y8fPn/B9L/JL1wViEzudDk8ch/LODjqfM59OyT24z8NyGbfjrvq11gFw2e+zYVn87P7ix/k5w26Xf9MZm5s5ev1+5FdyhcQDUUARFa7HE1JL95z7PinLAg1z7uggNLFULHHV6zHzfGaBAcsiv7XF9WBAqVhA7zIxVq04DqP+JX+6vwkCg+fN+X5xwXhwxVOncreJIuErZGybw8M3PCoV8f6OmYyG5O5lOXj1eukZE4GVOYhh2zbVapXd6mLkV7Ccm0yiKopiWelbScsQua0DUd41GqhotM7RWqugomEBlWjNE4H/IEVCiAG6tNYAAAAASUVORK5CYII=',
            // },
        }

        // load state
        chrome.storage.local.get(['downloads', 'settings', 'icons'], res => {
            if (res.downloads != undefined) {
                this.items = res.downloads
            }
            if (res.settings != undefined) {
                this.settings = res.settings
            } else {
                this.settings = this.settingsDefault
            }
            if (res.icons != undefined) {
                this.icons = res.icons
            }
        })

        // add orders sent to core, expecting response
        this.pending = {}
        // to change url
        this.urlPending = undefined

        this.updateBadge()

        const msgHandlers = {
            info: this.handleInfo.bind(this),
            add: this.handleAdd.bind(this),
            pause: this.handlePause.bind(this),
            completed: this.handleCompleted.bind(this),
            failed: this.handleFailed.bind(this),
            'pause-all': this.handlePauseAll.bind(this),
            error: this.handleError.bind(this),
            default: message => {
                notify('Error', 'Unknown message type: ' + message.type)
            },
        }

        this.catchDownload = this.catchDownload.bind(this)

        this.native = chrome.runtime.connectNative('com.k1dv5.dman')
        this.native.onDisconnect.addListener(() => {
            chrome.downloads.onChanged.removeListener(this.catchDownload)
            if (chrome.runtime.lastError) {
                notify('Please copy the ID and paste it in the dman prompt.', 'Then reload the extension.', undefined, 7000)
            }
        })
        this.native.onMessage.addListener(message => {
            (msgHandlers[message.type] || msgHandlers.default)(message)
        })

        // catch downloads
        chrome.downloads.onChanged.addListener(this.catchDownload)
    }

    // managing icons by keys
    addIconKey(hash, downCount) {
        if (hash in this.icons) {
            this.icons[hash].downloads += downCount
            if (this.icons[hash].downloads == 0) {
                delete this.icons[hash]
            }
        } else if (downCount > 0) {
            this.icons[hash] = {
                downloads: downCount,
            }
        }
    }

    add(browserId, url, dir, iconHash) {
        let id = Number(new Date().getTime().toString().slice(3, -2))
        this.pending[id] = {
            browserId,
            icon: iconHash,
        }
        // send to native
        this.native.postMessage({
            type: 'add',
            id,
            url,
            dir,
            conns: this.settings.conns,
        })
    }

    changeState(id, to) {
        let info = this.items[id]
        if (info.state == states.rebuilding) return  // rebuilding
        if (info.state == states.downloading) {  // downloading
            if (to != states.paused) return
            // pause
            this.native.postMessage({ id, type: 'pause', id })
        } else if (to == null) {  // delete
            this.native.postMessage({ id, type: 'delete', filename: info.filename, dir: info.dir })
        } else {  // paused / failed
            if (to != states.downloading) return
            // resume
            this.native.postMessage({
                id,
                type: 'add',
                url: info.url,
                filename: info.filename,  // filename will be used to know if resuming
                dir: info.dir
            })
        }
    }

    pauseAll() {
        this.native.postMessage({ type: 'pause-all' })
    }

    switchUpdates(to) {
        this.native.postMessage({ type: 'info', info: to })
    }

    openFile(id) {
        let down = this.items[id]
        this.native.postMessage({ type: 'open', filename: down.filename, dir: down.dir })
    }

    openDir(id) {
        this.native.postMessage({ type: 'open', dir: this.items[id]?.dir })
    }

    remove(id) {
        let download = this.items[id]
        if (download == undefined || progStates.includes(download.state)) {  // in progress, cannot remove
            return false
        }
        if (download.state == states.urlPending) {
            this.urlPending = undefined
        }
        if (download.state != states.completed) {
            this.native.postMessage({ id, type: 'remove', dir: download.dir, filename: download.filename })
        }
        this.addIconKey(this.items[id].icon, -1)
        delete this.items[id]
        chrome.storage.local.set({ downloads: this.items, icons: this.icons })
        return true
    }

    updateBadge() {
        let downs = Object.values(this.items).filter(d => progStates.includes(d.state)).length
        chrome.browserAction.setBadgeText({ text: String(downs || '') })
    }

    handleInfo(message) {
        let popup = chrome.extension.getViews({ type: 'popup' })[0]
        for (let stat of message.stats) {
            let download = this.items[stat.id]
            download.percent = stat.percent
            if (stat.rebuilding) {
                download.state = states.rebuilding
                continue
            }
            download.written = stat.written
            download.conns = stat.conns || 0
            download.eta = stat.eta
            download.speed = stat.speed
            popup?.update(stat.id)
        }
        if (popup == undefined) {
            this.switchUpdates(false)
        }
    }

    handleAdd(message) {
        if (message.error) {
            if (this.pending[message.id] != undefined) {
                chrome.downloads.search({ id: this.pending[message.id].id }, items => {
                    this.addIconKey(this.pending[message.id].icon, -1)
                    delete this.pending[message.id]
                    chrome.downloads.resume(items[0].id)
                    notify("Adding download failed", message.error + "\n\nContinuing in Downloads...", message.id, notifyTimeout)
                })
            } else {
                notify("Adding download failed", message.error, message.id, notifyTimeout)
            }
            return
        }
        let popup = chrome.extension.getViews({ type: 'popup' })[0]
        if (this.items[message.id] == undefined) {  // new download
            let download = {
                state: states.downloading,
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
                icon: this.pending[message.id].icon,
            }
            this.items[message.id] = download
            if (popup) {
                popup.add(message.id, download)  // popup.addRow
                this.switchUpdates(true)
            }
            chrome.downloads.erase({ id: this.pending[message.id].browserId }, () => {
                delete this.pending[message.id]
                if (this.settings.notify.begin) {
                    notify('Downloading file', message.filename, message.id, notifyTimeout)
                }
            })
        } else if (this.items[message.id].filename != message.filename) {  // resuming
            notify("Resume error", "Filenames don't match", message.id, notifyTimeout)
        } else {
            this.items[message.id].state = states.downloading
            if (popup) {
                popup.update(message.id)  // popup.addRow
                this.switchUpdates(true)
            }
        }
        this.updateBadge()
        chrome.storage.local.set({ downloads: this.items, icons: this.icons })
    }

    handlePause(message) {
        this.items[message.id].state = states.paused
        chrome.extension.getViews({ type: 'popup' })[0]?.update(message.id)  // popup.update
        chrome.storage.local.set({ downloads: this.items })
        this.updateBadge()
        if (message.error != undefined) {
            notify(message.error, this.items[message.id].filename, message.id, notifyTimeout)
        }
    }

    handleFailed(message) {
        this.items[message.id].state = states.failed
        notify('Download failed', this.items[message.id].filename + '\n' + message.error, message.id)  // keep message
        chrome.extension.getViews({ type: 'popup' })[0]?.update(message.id)  // popup.update
        chrome.storage.local.set({ downloads: this.items })
        this.updateBadge()
    }

    handleCompleted(message) {
        let download = this.items[message.id]
        download.state = states.completed
        if (!download.length && message.length) {
            download.length = message.length
        }
        if (message.filename) {
            download.filename = message.filename
        }
        this.updateBadge()
        chrome.extension.getViews({ type: 'popup' })[0]?.update(message.id)  // popup.update
        for (let stat of ['percent', 'written', 'speed', 'eta', 'conns']) {
            delete download[stat]
        }
        if (Object.values(this.items).filter(d => progStates.includes(d.state)).length == 0) {
            this.switchUpdates(false)
        }
        chrome.storage.local.set({ downloads: this.items })
        if (this.settings.notify.end) {
            notify('Download finished', download.filename, message.id, notifyTimeout)
        }
    }

    handlePauseAll(message) {
        let popup = chrome.extension.getViews({ type: 'popup' })[0]
        for (let stat of message.stats) {
            this.items[stat.id].state = states.paused
            popup?.update(ids)  // popup.update
        }
        chrome.storage.local.set({ downloads: this.items })
        this.updateBadge()
    }

    handleError(message) {
        notify('Error', message.error, message.id)
    }

    findDir(filePath) {
        let dirEnd = filePath.lastIndexOf(pathSep)
        let dir = filePath.slice(0, dirEnd)
        let fname = filePath.slice(dirEnd)
        let extStart = fname.lastIndexOf('.')
        if (extStart < 0) {
            dir = dir
        } else {
            let extension = fname.slice(extStart + 1)
            main: for (let [category, extensions] of Object.entries(this.settings.categories)) {
                for (let ext of extensions) {
                    if (ext == extension) {
                        dir += pathSep + category
                        break main
                    }
                }
            }
        }
        return dir
    }

    catchDownload(item) {
        if (item.filename == undefined) {
            return
        }
        chrome.downloads.pause(item.id, () => {
            if (this.urlPending) {  // refresh url for the waiting one and resume
                chrome.downloads.search({ id: item.id }, items => {
                    item = items[0]
                    let down = this.items[this.urlPending]
                    down.url = item.finalUrl
                    this.changeState(this.urlPending, states.downloading)
                    chrome.downloads.erase({ id: this.urlPending }, () => {
                        this.urlPending = undefined
                    })
                })
                return
            }
            let dir = this.findDir(item.filename.current)
            chrome.downloads.search({ id: item.id }, items => {
                item = items[0]
                chrome.downloads.getFileIcon(item.id, { size: 16 }, iconUrl => {
                    // store icon in the icons registry
                    let iconHash = hash32(iconUrl)
                    this.addIconKey(iconHash, 1)
                    this.icons[iconHash].url = iconUrl
                    this.add(item.id, item.finalUrl, dir, iconHash)
                })
            })
        })
    }

}

downloads = new Downloads()
