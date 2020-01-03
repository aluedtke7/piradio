## Preparing the Raspberry Zero W

### Create Micro-SD card
Download the Raspbian Buster Lite image from [this site](https://www.raspberrypi.org/downloads/raspbian/).
Unzip the downloaded file and insert an empty sd card into the card reader.
Check which device is responsible for this card:

    lsblk -p
    
This will give a list with a lot of devices. In my case, the last line showed

    /dev/sdc      8:32   1   3,7G  0 disk

This is the 4GB sd card I'm using. Now enter the following command to copy the image 
to the sd card:

    sudo dd bs=4M if=2019-09-26-raspbian-buster-lite.img of=/dev/sdc status=progress conv=fsync
    
This will take some time. In my case it took 226 seconds. Run `sync` in order to flush
all cached data to the sd card.

### First Boot
Insert the sd card in the sd card slot on the backside of the zero w. Connect a diasplay and a keyboard
via the HDMI mini and USB micro connectors and connect the power via the second micro USB connector.
