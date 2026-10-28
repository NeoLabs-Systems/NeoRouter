import 'dart:async';
import 'dart:js_interop';
import 'dart:typed_data';

import 'package:web/web.dart' as web;

/// Browser helpers for the playground's media modes: play audio, save a
/// file and pick a local file. The dashboard only runs on the web.
class AudioHandle {
  AudioHandle._(this._element, this._url);

  final web.HTMLAudioElement _element;
  final String _url;

  /// Completes when playback ends or is stopped.
  final Completer<void> done = Completer<void>();

  void stop() {
    _element.pause();
    _finish();
  }

  void _finish() {
    if (!done.isCompleted) {
      web.URL.revokeObjectURL(_url);
      done.complete();
    }
  }
}

String _objectUrl(Uint8List bytes, String mime) => web.URL.createObjectURL(web.Blob(<JSAny>[bytes.toJS].toJS, web.BlobPropertyBag(type: mime)));

AudioHandle playAudio(Uint8List bytes, String mime) {
  final url = _objectUrl(bytes, mime);
  final element = web.HTMLAudioElement()..src = url;
  final handle = AudioHandle._(element, url);
  element.onEnded.listen((_) => handle._finish());
  element.onError.listen((_) => handle._finish());
  element.play().toDart.catchError((Object _) {
    handle._finish();
    return null;
  });
  return handle;
}

void downloadBytes(Uint8List bytes, String mime, String filename) {
  final url = _objectUrl(bytes, mime);
  final anchor = web.HTMLAnchorElement()
    ..href = url
    ..download = filename;
  web.document.body?.append(anchor);
  anchor.click();
  anchor.remove();
  Timer(const Duration(seconds: 5), () => web.URL.revokeObjectURL(url));
}

typedef PickedFile = ({Uint8List bytes, String name, String mime});

/// Opens the browser's file dialog. Returns null when nothing was chosen.
Future<PickedFile?> pickFile({String accept = '*/*'}) {
  final completer = Completer<PickedFile?>();
  final input = web.HTMLInputElement()
    ..type = 'file'
    ..accept = accept;
  input.onChange.listen((_) async {
    final file = input.files?.item(0);
    if (file == null) return completer.complete(null);
    final buffer = await file.arrayBuffer().toDart;
    completer.complete((bytes: buffer.toDart.asUint8List(), name: file.name, mime: file.type));
  });
  // The 'cancel' event (no typed getter in package:web) fires when the dialog is dismissed.
  web.EventStreamProvider<web.Event>('cancel').forTarget(input).listen((_) {
    if (!completer.isCompleted) completer.complete(null);
  });
  input.click();
  return completer.future;
}
