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
            {"quality": "720p", "bitrate": 2500, "width": 1280, "height": 720, "s3_key": "renditions/vid-1/720p.mp4"}
        ]
        cons.update_status("vid-1", "ready", renditions)
        assert mock_patch.call_args[1]["json"]["renditions"] == renditions


def test_process_message_success():
    body = json.dumps({"video_id": "vid-123", "s3_key": "raw/vid-123/original.mp4", "owner_id": "owner1"}).encode()
    fake_minio = MagicMock()
    fake_minio.stat_object.return_value = MagicMock()
    fake_minio.copy_object.return_value = None

    with patch("app.consumer.get_minio", return_value=fake_minio), patch("app.consumer.update_status") as mock_status, patch(
        "app.consumer.time.sleep"
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
        assert fake_minio.copy_object.call_count == 3


def test_process_message_invalid_json():
    with patch("app.consumer.update_status") as mock_status, patch("app.consumer.get_minio") as mock_get:
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

    with patch("app.consumer.get_minio", return_value=fake_minio), patch("app.consumer.update_status") as mock_status, patch(
        "app.consumer.time.sleep"
    ):
        cons.process_message(body)
        # first processing, then failed
        assert mock_status.call_args_list[0][0][1] == "processing"
        assert mock_status.call_args_list[1][0][1] == "failed"


def test_process_message_copy_failure_marks_failed():
    body = json.dumps({"video_id": "vid-1", "s3_key": "raw/vid-1/original.mp4"}).encode()
    fake_minio = MagicMock()
    fake_minio.stat_object.return_value = MagicMock()
    fake_minio.copy_object.side_effect = Exception("copy error")

    with patch("app.consumer.get_minio", return_value=fake_minio), patch("app.consumer.update_status") as mock_status, patch(
        "app.consumer.time.sleep"
    ):
        cons.process_message(body)
        assert mock_status.call_args_list[-1][0][1] == "failed"
