#import <AVFoundation/AVFoundation.h>

// meetsieve_audio_authorization_status 返回 AVFoundation 的麦克风授权枚举值。
int meetsieve_audio_authorization_status(void) {
    if (@available(macOS 10.14, *)) {
        return (int)[AVCaptureDevice authorizationStatusForMediaType:AVMediaTypeAudio];
    }
    // macOS 10.13 没有该授权查询 API，继续由 CoreAudio 返回原生权限结果。
    return 3;
}
