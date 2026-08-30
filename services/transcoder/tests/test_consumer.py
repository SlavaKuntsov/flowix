import json
from unittest.mock import MagicMock, patch

import app.consumer as cons


def test_update_status_payload():
    with patch("app.consumer.requests.patch") as mock_patch:
        mock_resp = MagicMock()
        mock_resp.raise_for_status.return_value = None
        mock_patch.return_value = mock_resp

        cons.update_status("vid-1", "processing")
        mock_patch.assert_called_once()
        url, kwargs = mock_patch.call_args[0][0], mock_patch.call_args[1]
        assert url.endswith("/internal/videos/vid-1/status")
        assert kwargs["json"]["status"] == "processing"

        mock_patch.reset_mock()
        renditions = [
            {
                "quality": "720p",
                "bitrate": 2500,
                "width": 1280,
                "height": 720,
                "s3_key": "renditions/vid-1/720p.mp4",
            }
        ]
        cons.update_status("vid-1", "ready", renditions)
        assert mock_patch.call_args[1]["json"]["renditions"] == renditions


def test_process_message_success():
    body = json.dumps(
        {"video_id": "vid-123", "s3_key": "raw/vid-123/original.mp4", "owner_id": "owner1"}
    ).encode()
    fake_minio = MagicMock()
    fake_minio.stat_object.return_value = MagicMock()
    fake_minio.fget_object.return_value = None
    fake_minio.fput_object.return_value = None

    with (
        patch("app.consumer.get_minio", return_value=fake_minio),
        patch("app.consumer.update_status") as mock_status,
        patch("app.consumer.probe_video", return_value={}),
        patch("app.consumer.encode_audio", return_value=None),
        patch("app.consumer.transcode_one", return_value=None),
        patch("app.consumer.transcode_thumbnail", return_value=None),
    ):
        cons.process_message(body)

        # processing then ready
        assert mock_status.call_count == 2
        assert mock_status.call_args_list[0][0] == ("vid-123", "processing")
        assert mock_status.call_args_list[1][0][0] == "vid-123"
        assert mock_status.call_args_list[1][0][1] == "ready"
        renditions = mock_status.call_args_list[1][0][2]
        assert len(renditions) == 3
        assert {r["quality"] for r in renditions} == {"360p", "720p", "1080p"}
        # 3 renditions uploaded via fput (thumbnail maybe filtered)
        assert fake_minio.fput_object.call_count == 3
        fake_minio.fget_object.assert_called_once()


def test_process_message_invalid_json():
    with (
        patch("app.consumer.update_status") as mock_status,
        patch("app.consumer.get_minio") as mock_get,
    ):
        cons.process_message(b"not json")
        mock_status.assert_not_called()
        mock_get.assert_not_called()


def test_process_message_missing_fields():
    body = json.dumps({"s3_key": "raw/x/original.mp4"}).encode()
    with patch("app.consumer.update_status") as mock_status:
        cons.process_message(body)
        mock_status.assert_not_called()


def test_process_message_minio_failure_marks_failed():
    body = json.dumps({"video_id": "vid-1", "s3_key": "raw/vid-1/original.mp4"}).encode()
    fake_minio = MagicMock()
    fake_minio.stat_object.side_effect = Exception("not found")

    with (
        patch("app.consumer.get_minio", return_value=fake_minio),
        patch("app.consumer.update_status") as mock_status,
    ):
        cons.process_message(body)
        # first processing, then failed
        assert mock_status.call_args_list[0][0][1] == "processing"
        assert mock_status.call_args_list[1][0][1] == "failed"


def test_process_message_transcode_failure_marks_failed():
    body = json.dumps({"video_id": "vid-1", "s3_key": "raw/vid-1/original.mp4"}).encode()
    fake_minio = MagicMock()
    fake_minio.stat_object.return_value = MagicMock()
    fake_minio.fget_object.return_value = None

    with (
        patch("app.consumer.get_minio", return_value=fake_minio),
        patch("app.consumer.update_status") as mock_status,
        patch("app.consumer.probe_video", return_value={}),
        patch("app.consumer.encode_audio", return_value=None),
        patch("app.consumer.transcode_one", side_effect=Exception("ffmpeg error")),
        patch("app.consumer.transcode_thumbnail", return_value=None),
    ):
        cons.process_message(body)
        assert mock_status.call_args_list[-1][0][1] == "failed"


def test_transcode_one_builds_aligned_cmd():
    with patch("app.consumer.subprocess.run") as mock_run:
        mock_run.return_value = MagicMock()
        cons.transcode_one("/tmp/in.mp4", "/tmp/audio.m4a", "/tmp/out.mp4", 1280, 720, 2500)
        cmd = mock_run.call_args[0][0]
        assert "ffmpeg" in cmd
        assert "-force_key_frames" in cmd
        assert "expr:gte(t,n_forced*2)" in cmd
        assert "-g" in cmd and "60" in cmd
        assert "-r" in cmd and "30" in cmd
        assert "-sc_threshold" in cmd and "0" in cmd
        assert "scale=-2:720" in " ".join(cmd)
        # shared audio track is copied, never re-encoded per rendition
        assert "/tmp/audio.m4a" in cmd
        assert cmd[cmd.index("-c:a") + 1] == "copy"
        assert "-map" in cmd and "1:a:0" in cmd


def test_transcode_one_without_audio_is_video_only():
    with patch("app.consumer.subprocess.run") as mock_run:
        mock_run.return_value = MagicMock()
        cons.transcode_one("/tmp/in.mp4", None, "/tmp/out.mp4", 1280, 720, 2500)
        cmd = mock_run.call_args[0][0]
        assert "-an" in cmd
        assert "-c:a" not in cmd


def test_encode_audio_single_normalized_track():
    with patch("app.consumer.subprocess.run") as mock_run:
        mock_run.return_value = MagicMock()
        cons.encode_audio("/tmp/in.mp4", "/tmp/audio.m4a")
        cmd = mock_run.call_args[0][0]
        assert "-vn" in cmd
        assert cmd[cmd.index("-c:a") + 1] == "aac"
        assert cmd[cmd.index("-b:a") + 1] == cons.AUDIO_BITRATE
        # fixed sample rate / channel layout keeps every rendition byte-identical
        assert cmd[cmd.index("-ar") + 1] == "48000"
        assert cmd[cmd.index("-ac") + 1] == "2"


def test_process_message_shares_one_audio_track():
    body = json.dumps({"video_id": "vid-9", "s3_key": "raw/vid-9/original.mp4"}).encode()
    fake_minio = MagicMock()

    with (
        patch("app.consumer.get_minio", return_value=fake_minio),
        patch("app.consumer.update_status"),
        patch("app.consumer.probe_video", return_value={}),
        patch("app.consumer.encode_audio") as mock_audio,
        patch("app.consumer.transcode_one") as mock_transcode,
        patch("app.consumer.transcode_thumbnail", return_value=None),
    ):
        cons.process_message(body)

        mock_audio.assert_called_once()
        audio_path = mock_audio.call_args[0][1]
        assert mock_transcode.call_count == 3
        # every rendition stream-copies the same audio file
        assert {c[0][1] for c in mock_transcode.call_args_list} == {audio_path}


def test_process_message_audio_failure_falls_back_to_video_only():
    body = json.dumps({"video_id": "vid-8", "s3_key": "raw/vid-8/original.mp4"}).encode()
    fake_minio = MagicMock()

    with (
        patch("app.consumer.get_minio", return_value=fake_minio),
        patch("app.consumer.update_status") as mock_status,
        patch("app.consumer.probe_video", return_value={}),
        patch("app.consumer.encode_audio", side_effect=Exception("no audio stream")),
        patch("app.consumer.transcode_one") as mock_transcode,
        patch("app.consumer.transcode_thumbnail", return_value=None),
    ):
        cons.process_message(body)

        assert mock_status.call_args_list[-1][0][1] == "ready"
        assert {c[0][1] for c in mock_transcode.call_args_list} == {None}


def test_probe_video_handles_missing_ffprobe():
    with patch("app.consumer.subprocess.run", side_effect=FileNotFoundError("no ffprobe")):
        assert cons.probe_video("/tmp/x.mp4") is None
