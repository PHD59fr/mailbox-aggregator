// jsdom's File/Blob implementation is older than the browser APIs used by the
// application. Keep the tests on the same code paths as real browsers.
if (typeof File !== 'undefined' && typeof File.prototype.arrayBuffer !== 'function') {
  File.prototype.arrayBuffer = function (this: File) {
    return new Promise<ArrayBuffer>((resolve, reject) => {
      const reader = new FileReader()
      reader.onload = () => {
        if (reader.result instanceof ArrayBuffer) resolve(reader.result)
        else reject(new Error('Could not read the file.'))
      }
      reader.onerror = () => reject(reader.error)
      reader.onabort = () => reject(new Error('Reading the attachment was cancelled.'))
      reader.readAsArrayBuffer(this)
    })
  }
}

if (typeof File !== 'undefined' && typeof File.prototype.text !== 'function') {
  File.prototype.text = function (this: File) {
    return new Promise<string>((resolve, reject) => {
      const reader = new FileReader()
      reader.onload = () => resolve(typeof reader.result === 'string' ? reader.result : '')
      reader.onerror = () => reject(reader.error)
      reader.onabort = () => reject(new Error('Reading the attachment was cancelled.'))
      reader.readAsText(this)
    })
  }
}
