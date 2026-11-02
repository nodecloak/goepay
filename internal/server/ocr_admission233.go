package server

// Full image decoding may allocate eight bytes per pixel. Bound concurrent
// OCR bodies/decoders/provider calls independently of payment request traffic.
var ocrAdmission233 = make(chan struct{}, 2)
