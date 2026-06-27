package usecase

// Expose pembentuk key rate limiter (unexported) ke test eksternal (usecase_test) agar test bisa
// menyetel batas limiter pada key yang sama persis dengan yang dipakai use case.
var (
	OTPRequestKeyForTest = otpRequestKey
	OTPVerifyKeyForTest  = otpVerifyKey
)
