// Electron's native message box waits for a person. Required into the packaged
// main process before the application module runs, this records every question
// it asks and holds each one open until the test answers it.
const { dialog } = require('electron')

const recorder = { questions: [], answer: undefined }
globalThis.nativeQuestions = recorder

dialog.showMessageBox = (...args) => {
  // The options come last whether or not a parent window is passed first.
  recorder.questions.push(args.at(-1))
  return new Promise((resolve) => {
    // Settle after the answering evaluation has replied: answering Quit ends
    // the process, and that exit must not race the reply to the test.
    recorder.answer = (response) =>
      setImmediate(() => resolve({ response, checkboxChecked: false }))
  })
}
